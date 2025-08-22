package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"sort"
	"time"

	"github.com/cespare/xxhash/v2"
	_ "github.com/mattn/go-sqlite3"
	pb "github.com/puter/fs_tree_manager/go"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
)

type server struct {
	pb.UnimplementedFSTreeManagerServer
	db *sql.DB
}

// calculateMerkleHash calculates the MerkleHash for a node based on its attributes and children hashes
func calculateMerkleHash(node *pb.MerkleTree) string {
	// Create a hash object
	hasher := xxhash.New()

	// Add self attributes to the hash
	// We'll hash the metadata as JSON to include all self attributes
	if node.Metadata != nil {
		metadataBytes, err := json.Marshal(node.Metadata.AsMap())
		if err == nil {
			hasher.Write(metadataBytes)
		}
	}

	// Add name to the hash
	hasher.WriteString(node.Name)

	// Add children hashes in sorted order for consistency
	childrenHashes := make([]string, len(node.Children))
	for i, child := range node.Children {
		childrenHashes[i] = child.MerkleHash
	}
	sort.Strings(childrenHashes)

	for _, childHash := range childrenHashes {
		hasher.WriteString(childHash)
	}

	// Return the hash as a hex string
	return fmt.Sprintf("%x", hasher.Sum64())
}

// calculateTreeMerkleHashes recursively calculates MerkleHash for all nodes in the tree (bottom-up)
func calculateTreeMerkleHashes(node *pb.MerkleTree) {
	// First, calculate hashes for all children
	for _, child := range node.Children {
		calculateTreeMerkleHashes(child)
	}

	// Then calculate hash for this node (now that children have their hashes)
	node.MerkleHash = calculateMerkleHash(node)
}

// FetchReplica implements the FSTreeManager service
func (s *server) FetchReplica(ctx context.Context, req *pb.FetchReplicaRequest) (*pb.FetchReplicaResponse, error) {
	log.Printf("=== gRPC Request Received ===")
	log.Printf("Method: FetchReplica")
	log.Printf("User: %s", req.UserName)
	log.Printf("Timestamp: %s", time.Now().Format(time.RFC3339))
	log.Printf("=============================")

	// Query the database for the user's filesystem entries
	tree, err := s.buildUserFSTree(req.UserName)
	if err != nil {
		log.Printf("Error building FS tree for user %s: %v", req.UserName, err)
		return nil, err
	}

	log.Printf("Sending response with MerkleTree with name: %s", tree.Name)

	return &pb.FetchReplicaResponse{
		Tree: tree,
	}, nil
}

// TODO: compatible with path search:
// https://github.com/HeyPuter/puter/blob/0c60ebd1d066030349fe33bcee1d70de5b81110c/src/backend/src/boot/RuntimeEnvironment.js#L133-L162
//
// const sqliteDBPath = "../../volatile/runtime/puter-database.sqlite"
const sqliteDBPath = "/var/puter/puter-database.sqlite"

const tableName = "fsentries"

// buildMetadata creates a comprehensive metadata structure matching the expected format
func buildMetadata(uuid, name, path, parentUID, userName string, isDir bool, size sql.NullInt64,
	createdAt, modifiedAt, accessedAt float64, isPublic, isShortcut, isSymlink sql.NullBool,
	symlinkPath, sortBy, sortOrder sql.NullString, immutable sql.NullBool,
	metadata, associatedAppID, publicToken, fileRequestToken sql.NullString) (*structpb.Struct, error) {

	// Calculate dirname and dirpath
	dirname := filepath.Dir(path)
	dirpath := dirname

	// Determine if empty (for directories, check if they have children)
	isEmpty := true
	if isDir {
		// This would need to be determined by checking if the directory has children
		// For now, we'll set it based on size or other heuristics
		isEmpty = !size.Valid || size.Int64 == 0
	}

	// Build the metadata map
	metadataMap := map[string]interface{}{
		"is_empty":           isEmpty,
		"id":                 uuid,
		"associated_app_id":  getStringValue(associatedAppID),
		"public_token":       getStringValue(publicToken),
		"file_request_token": getStringValue(fileRequestToken),
		"parent_uid":         parentUID,
		"is_dir":             isDir,
		"is_public":          getBoolValue(isPublic),
		"is_shortcut":        getIntValue(isShortcut),
		"is_symlink":         getIntValue(isSymlink),
		"symlink_path":       getStringValue(symlinkPath),
		"sort_by":            getStringValue(sortBy),
		"sort_order":         getStringValue(sortOrder),
		"immutable":          getIntValue(immutable),
		"name":               name,
		"metadata":           getStringValue(metadata),
		"modified":           int64(modifiedAt),
		"created":            int64(createdAt),
		"accessed":           int64(accessedAt),
		"size":               getInt64Value(size),
		"layout":             nil,
		"path":               path,
		"owner": map[string]interface{}{
			"username": userName,
		},
		"type":       nil,
		"subdomains": []interface{}{},
		"shares": map[string]interface{}{
			"users": []interface{}{},
			"apps":  []interface{}{},
		},
		"versions":  []interface{}{},
		"dirname":   dirname,
		"dirpath":   dirpath,
		"writable":  true, // Default to true, could be calculated based on permissions
		"parent_id": parentUID,
		"uid":       uuid,
	}

	return structpb.NewStruct(metadataMap)
}

// Helper functions to handle nullable values
func getStringValue(ns sql.NullString) interface{} {
	if ns.Valid {
		return ns.String
	}
	return nil
}

func getBoolValue(nb sql.NullBool) interface{} {
	if nb.Valid {
		return nb.Bool
	}
	return nil
}

func getIntValue(nb sql.NullBool) int {
	if nb.Valid && nb.Bool {
		return 1
	}
	return 0
}

func getInt64Value(ni sql.NullInt64) interface{} {
	if ni.Valid {
		return ni.Int64
	}
	return nil
}

// buildUserFSTree builds the filesystem tree for a given user from the database
func (s *server) buildUserFSTree(userName string) (*pb.MerkleTree, error) {
	rootPath := "/" + userName

	// Single query to get all filesystem entries for the user
	query := `
		SELECT uuid, name, is_dir, size, created, modified, path, parent_uid, 
		       is_public, is_shortcut, is_symlink, symlink_path, sort_by, sort_order,
		       immutable, metadata, accessed, associated_app_id, public_token, file_request_token
		FROM fsentries 
		WHERE path LIKE ?
	`

	rows, err := s.db.Query(query, rootPath+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Create a map to store all nodes by UUID for efficient lookup
	nodeMap := make(map[string]*pb.MerkleTree)
	// Map to store parent-child relationships
	parentChildMap := make(map[string][]string)

	// Find the actual user root directory (should be exactly "/username")
	var rootTree *pb.MerkleTree

	// Process all entries
	for rows.Next() {
		var uuid, name, path string
		var parentUID sql.NullString
		var isDir bool
		var size sql.NullInt64
		var createdAt, modifiedAt float64
		var accessedAt sql.NullFloat64
		var isPublic, isShortcut, isSymlink, immutable sql.NullBool
		var symlinkPath, sortBy, sortOrder, metadata, associatedAppID, publicToken, fileRequestToken sql.NullString

		err := rows.Scan(&uuid, &name, &isDir, &size, &createdAt, &modifiedAt, &path, &parentUID,
			&isPublic, &isShortcut, &isSymlink, &symlinkPath, &sortBy, &sortOrder,
			&immutable, &metadata, &accessedAt, &associatedAppID, &publicToken, &fileRequestToken)
		if err != nil {
			log.Printf("Error scanning row: %v", err)
			continue
		}

		// Build comprehensive metadata
		parentUIDStr := ""
		if parentUID.Valid {
			parentUIDStr = parentUID.String
		}

		// Handle accessedAt - use current time if NULL
		accessedAtValue := float64(time.Now().Unix())
		if accessedAt.Valid {
			accessedAtValue = accessedAt.Float64
		}

		metadataStruct, err := buildMetadata(uuid, name, path, parentUIDStr, userName, isDir, size,
			createdAt, modifiedAt, accessedAtValue, isPublic, isShortcut, isSymlink,
			symlinkPath, sortBy, sortOrder, immutable, metadata, associatedAppID, publicToken, fileRequestToken)
		if err != nil {
			log.Printf("Error building metadata for %s: %v", uuid, err)
			continue
		}

		// Create the node node (MerkleHash will be calculated later)
		node := &pb.MerkleTree{
			Name:       name,
			MerkleHash: "", // Will be calculated after children are set
			Metadata:   metadataStruct,
			Children:   []*pb.MerkleTree{},
		}

		// Store the node in the map
		nodeMap[uuid] = node

		// Track parent-child relationships
		if parentUID.Valid {
			parentChildMap[parentUID.String] = append(parentChildMap[parentUID.String], uuid)
		}

		// Check if this is the root directory by looking at the path in metadata
		if path == rootPath {
			rootTree = node
			continue
		}
	}

	// Build the tree structure by linking children to parents
	for parentUUID, childUUIDs := range parentChildMap {
		if parent, exists := nodeMap[parentUUID]; exists {
			for _, childUUID := range childUUIDs {
				if child, exists := nodeMap[childUUID]; exists {
					parent.Children = append(parent.Children, child)
				}
			}
		}
	}

	// If no root directory found, return error
	if rootTree == nil {
		return nil, fmt.Errorf("user root directory not found: %s", rootPath)
	}

	// Calculate MerkleHash for all nodes in the tree (bottom-up)
	calculateTreeMerkleHashes(rootTree)

	return rootTree, nil
}

func main() {
	// Open database connection
	db, err := sql.Open("sqlite3", sqliteDBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Test database connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	log.Printf("Connected to database: %s", sqliteDBPath)

	// Create a TCP listener on port 50052
	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	// Create a gRPC server
	grpcServer := grpc.NewServer()

	// Register the FSTreeManager service with database connection
	pb.RegisterFSTreeManagerServer(grpcServer, &server{db: db})

	log.Println("FS Tree Manager server starting on port 50052...")
	log.Println("Server ready to handle FetchReplica requests from database")

	// Start the server
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
