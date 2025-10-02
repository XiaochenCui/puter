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
	"strconv"
	"strings"
	"time"

	"github.com/cespare/xxhash/v2"
	_ "github.com/mattn/go-sqlite3"
	pb "github.com/puter/fs_tree_manager/go"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type server struct {
	pb.UnimplementedFSTreeManagerServer
	db    *sql.DB
	trees map[string]*pb.MerkleTree // In-memory tree cache by username
}

// calculateMerkleHash calculates the MerkleHash for a node based on its attributes and children hashes
func calculateMerkleHash(node *pb.MerkleNode, childrenHashes []uint64) string {
	// Create a hash object
	hasher := xxhash.New()

	{
		metadataBytes, _ := json.Marshal(node.FsEntry.Metadata.AsMap())
		log.Printf("debug_metadata: %s", string(metadataBytes))
		debugHasher := xxhash.New()
		debugHasher.Write(metadataBytes)
		debugHash := debugHasher.Sum64()
		log.Printf("debug_hash: %d", debugHash)
	}

	// Add self attributes to the hash
	// We'll hash the metadata as JSON to include all self attributes
	if node.FsEntry.Metadata != nil {
		metadataBytes, err := json.Marshal(node.FsEntry.Metadata.AsMap())
		log.Printf("metadataBytes: %s", string(metadataBytes))
		if err == nil {
			hasher.Write(metadataBytes)
		}
	}

	// Add children hashes in sorted order for consistency
	sort.Slice(childrenHashes, func(i, j int) bool {
		return childrenHashes[i] < childrenHashes[j]
	})
	log.Printf("childrenHashes [%d]: %v", len(childrenHashes), childrenHashes)

	for _, childHash := range childrenHashes {
		hasher.WriteString(fmt.Sprintf("%d", childHash))
	}

	hash := hasher.Sum64()
	hashStr := fmt.Sprintf("%d", hash)
	log.Printf("node %s hash: %s", node.FsEntry.Metadata.AsMap()["path"], hashStr)
	return hashStr
}

// calculateTreeMerkleHashes calculates MerkleHash for all nodes in the tree (bottom-up)
func calculateTreeMerkleHashes(tree *pb.MerkleTree) {
	// First pass: calculate hashes for leaf nodes (nodes with no children)
	for _, node := range tree.Nodes {
		if len(node.ChildrenUuids) == 0 {
			node.MerkleHash = calculateMerkleHash(node, []uint64{})
		}
	}

	// Second pass: calculate hashes for parent nodes (bottom-up)
	// We need to process nodes in order from leaves to root
	processed := make(map[string]bool)

	for {
		allProcessed := true
		for _, node := range tree.Nodes {
			if processed[node.Uuid] {
				continue
			}

			// Check if all children have been processed
			allChildrenProcessed := true
			childrenHashes := make([]uint64, 0, len(node.ChildrenUuids))
			for _, childID := range node.ChildrenUuids {
				if child, exists := tree.Nodes[childID]; exists {
					if !processed[childID] {
						allChildrenProcessed = false
						break
					}
					// Convert string hash back to uint64 for hashing
					if hashVal, err := strconv.ParseUint(child.MerkleHash, 10, 64); err == nil {
						childrenHashes = append(childrenHashes, hashVal)
					}
				}
			}

			if allChildrenProcessed {
				node.MerkleHash = calculateMerkleHash(node, childrenHashes)
				processed[node.Uuid] = true
			} else {
				allProcessed = false
			}
		}

		if allProcessed {
			break
		}
	}
}

// recalculateAncestorHashes recalculates Merkle hashes for all ancestors of a given node
func recalculateAncestorHashes(tree *pb.MerkleTree, nodeID string) {
	// Start from the current node and work up to the root
	currentNodeID := nodeID

	for currentNodeID != "" {
		currentNode, exists := tree.Nodes[currentNodeID]
		if !exists {
			break
		}

		childrenHashes := make([]uint64, 0, len(currentNode.ChildrenUuids))
		for _, childID := range currentNode.ChildrenUuids {
			if child, exists := tree.Nodes[childID]; exists && child.MerkleHash != "" {
				// Convert string hash back to uint64 for hashing
				if hashVal, err := strconv.ParseUint(child.MerkleHash, 10, 64); err == nil {
					childrenHashes = append(childrenHashes, hashVal)
				}
			}
		}

		currentNode.MerkleHash = calculateMerkleHash(currentNode, childrenHashes)

		currentNodeID = currentNode.ParentUuid
	}
}

// FetchReplica implements the FSTreeManager service
func (s *server) FetchReplica(ctx context.Context, req *pb.UserName) (*pb.MerkleTree, error) {
	log.Printf("=== gRPC Request Received ===")
	log.Printf("Method: FetchReplica")
	log.Printf("User: %s", req.UserName)
	log.Printf("Timestamp: %s", time.Now().Format(time.RFC3339))
	log.Printf("=============================")

	var tree *pb.MerkleTree
	var err error

	// Use in-memory tree if available, otherwise build from database
	if cachedTree, exists := s.trees[req.UserName]; exists {
		tree = cachedTree
		log.Printf("=== Using Cached Tree ===")
		log.Printf("Username: %s", req.UserName)
		log.Printf("Root UUID: %s", tree.RootUuid)
		log.Printf("Root Hash: %s", tree.Nodes[tree.RootUuid].MerkleHash)
		log.Printf("=========================")
	} else {
		// Query the database for the user's filesystem entries
		tree, err = s.buildUserFSTree(req.UserName)
		if err != nil {
			log.Printf("Error building FS tree for user %s: %v", req.UserName, err)
			return nil, err
		}
		// Cache the tree for future use
		s.trees[req.UserName] = tree
		log.Printf("=== Built and Cached Tree ===")
		log.Printf("Username: %s", req.UserName)
		log.Printf("Root UUID: %s", tree.RootUuid)
		log.Printf("Root Hash: %s", tree.Nodes[tree.RootUuid].MerkleHash)
		log.Printf("=============================")
	}

	log.Printf("=== MerkleTree Response ===")
	log.Printf("Username: %s", req.UserName)
	log.Printf("Root UUID: %s", tree.RootUuid)
	log.Printf("Root Hash: %s", tree.Nodes[tree.RootUuid].MerkleHash)
	log.Printf("=========================")

	return tree, nil
}

// extractUsernameFromPath extracts the username from a path like /admin/Desktop/New -> admin
func extractUsernameFromPath(path string) (string, error) {
	// Remove leading slash and split by path separators
	cleanPath := strings.TrimPrefix(path, "/")
	pathParts := strings.Split(cleanPath, "/")

	if len(pathParts) == 0 || pathParts[0] == "" {
		return "", fmt.Errorf("invalid path format: no username found in path %s", path)
	}

	username := pathParts[0]
	if username == "" {
		return "", fmt.Errorf("invalid path format: empty username in path %s", path)
	}

	return username, nil
}

// NewFSEntry implements the FSTreeManager service
func (s *server) NewFSEntry(ctx context.Context, req *pb.FSEntry) (*emptypb.Empty, error) {
	log.Printf("=== gRPC Request Received ===")
	log.Printf("Method: NewFSEntry")
	log.Printf("Metadata: %v", req.Metadata)
	log.Printf("Timestamp: %s", time.Now().Format(time.RFC3339))
	log.Printf("=============================")

	// Extract username from path
	metadataMap := req.Metadata.AsMap()
	path, ok := metadataMap["path"].(string)
	if !ok {
		log.Printf("Invalid metadata: missing path information")
		return nil, fmt.Errorf("invalid metadata: missing path information")
	}
	username, pathErr := extractUsernameFromPath(path)
	if pathErr != nil {
		log.Printf("Error extracting username from path %s: %v", path, pathErr)
		return nil, fmt.Errorf("error extracting username from path: %v", pathErr)
	}

	// Get or build tree for this user
	var tree *pb.MerkleTree
	var err error
	if cachedTree, exists := s.trees[username]; exists {
		tree = cachedTree
		log.Printf("=== Using Cached Tree ===")
		log.Printf("Username: %s", username)
		log.Printf("Root UUID: %s", tree.RootUuid)
		log.Printf("Root Hash: %s", tree.Nodes[tree.RootUuid].MerkleHash)
		log.Printf("=========================")
	} else {
		log.Printf("No cached tree found for user %s", username)
		tree, err = s.buildUserFSTree(username)
		if err != nil {
			log.Printf("Error building FS tree for user %s: %v", username, err)
			return nil, err
		}
		s.trees[username] = tree
		log.Printf("=== Built and Cached Tree ===")
		log.Printf("Username: %s", username)
		log.Printf("Root UUID: %s", tree.RootUuid)
		log.Printf("Root Hash: %s", tree.Nodes[tree.RootUuid].MerkleHash)
		log.Printf("=============================")
	}

	// Extract entry information from metadata
	uid, ok := metadataMap["uid"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid metadata: missing uid")
	}

	parentUID, ok := metadataMap["parent_uid"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid metadata: missing parent_uid")
	}

	// Find the parent directory
	parentNode, exists := tree.Nodes[parentUID]
	if !exists {
		return nil, fmt.Errorf("parent directory not found: %s", parentUID)
	}

	// Create new entry node
	newNode := &pb.MerkleNode{
		Uuid:          uid,
		MerkleHash:    "", // Will be calculated below
		ParentUuid:    parentUID,
		FsEntry:       req,
		ChildrenUuids: []string{},
	}

	// Add to nodes map
	tree.Nodes[uid] = newNode

	// Add to parent's children_uuids
	parentNode.ChildrenUuids = append(parentNode.ChildrenUuids, uid)

	// Calculate Merkle hash for the new entry (empty children)
	newNode.MerkleHash = calculateMerkleHash(newNode, []uint64{})

	// Recalculate Merkle hashes for all ancestors
	recalculateAncestorHashes(tree, uid)

	// Print root hash for this user
	rootNode, exists := tree.Nodes[tree.RootUuid]
	if exists {
		log.Printf("=== New FSEntry Created ===")
		log.Printf("Username: %s", username)
		log.Printf("Root UUID: %s", tree.RootUuid)
		log.Printf("Root Hash: %s", rootNode.MerkleHash)
		log.Printf("=============================")
	}

	log.Printf("Successfully created FSEntry %s in parent %s", uid, parentUID)
	return &emptypb.Empty{}, nil
}

// RemoveFSEntry implements the FSTreeManager service
func (s *server) RemoveFSEntry(ctx context.Context, req *pb.FSEntry) (*emptypb.Empty, error) {
	log.Printf("=== gRPC Request Received ===")
	log.Printf("Method: RemoveFSEntry")
	log.Printf("Metadata: %v", req.Metadata)
	log.Printf("Timestamp: %s", time.Now().Format(time.RFC3339))
	log.Printf("=============================")

	// Extract username from path
	metadataMap := req.Metadata.AsMap()
	path, ok := metadataMap["path"].(string)
	if !ok {
		log.Printf("Invalid metadata: missing path information")
		return nil, fmt.Errorf("invalid metadata: missing path information")
	}
	username, pathErr := extractUsernameFromPath(path)
	if pathErr != nil {
		log.Printf("Error extracting username from path %s: %v", path, pathErr)
		return nil, fmt.Errorf("error extracting username from path: %v", pathErr)
	}

	// Get tree for this user
	tree, exists := s.trees[username]
	if !exists {
		log.Printf("No cached tree found for user %s", username)
		return nil, fmt.Errorf("no cached tree found for user %s", username)
	}

	// Extract entry information from metadata
	uid, ok := metadataMap["uid"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid metadata: missing uid")
	}

	// Find the entry to remove
	node, exists := tree.Nodes[uid]
	if !exists {
		return nil, fmt.Errorf("entry not found: %s", uid)
	}

	// Remove from parent's children_uuids
	if node.ParentUuid != "" {
		if parentNode, parentExists := tree.Nodes[node.ParentUuid]; parentExists {
			// Remove the uid from parent's children list
			for i, childUUID := range parentNode.ChildrenUuids {
				if childUUID == uid {
					parentNode.ChildrenUuids = append(parentNode.ChildrenUuids[:i], parentNode.ChildrenUuids[i+1:]...)
					break
				}
			}
		}
	}

	// Remove the node from the tree
	delete(tree.Nodes, uid)

	// Recalculate Merkle hashes for all ancestors
	if node.ParentUuid != "" {
		recalculateAncestorHashes(tree, node.ParentUuid)
	}

	// Print root hash for this user
	rootNode, exists := tree.Nodes[tree.RootUuid]
	if exists {
		log.Printf("=== FSEntry Removed ===")
		log.Printf("Username: %s", username)
		log.Printf("Root UUID: %s", tree.RootUuid)
		log.Printf("Root Hash: %s", rootNode.MerkleHash)
		log.Printf("=============================")
	}

	log.Printf("Successfully removed FSEntry %s", uid)
	return &emptypb.Empty{}, nil
}

// PullDiff implements the FSTreeManager service
func (s *server) PullDiff(ctx context.Context, req *pb.PullRequest) (*pb.PushRequest, error) {
	log.Printf("=== gRPC Request Received ===")
	log.Printf("Method: PullDiff")
	log.Printf("Timestamp: %s", time.Now().Format(time.RFC3339))
	log.Printf("PullRequest Items Count: %d", len(req.PullRequest))

	// Print each pull request item
	for i, item := range req.PullRequest {
		log.Printf("  Item %d:", i+1)
		log.Printf("    UUID: %s", item.Uuid)
		log.Printf("    Merkle Hash: %s", item.MerkleHash)
	}
	log.Printf("=============================")

	// For now, return an empty PushRequest
	// In a real implementation, this would compare the requested items
	// with the current state and return the differences
	response := &pb.PushRequest{
		PushRequest: []*pb.PushRequestItem{},
	}

	log.Printf("=== PullDiff Response ===")
	log.Printf("PushRequest Items Count: %d", len(response.PushRequest))
	log.Printf("=========================")

	return response, nil
}

// PurgeReplica implements the FSTreeManager service
func (s *server) PurgeReplica(ctx context.Context, req *pb.UserName) (*emptypb.Empty, error) {
	log.Printf("=== gRPC Request Received ===")
	log.Printf("Method: PurgeReplica")
	log.Printf("User: %s", req.UserName)
	log.Printf("Timestamp: %s", time.Now().Format(time.RFC3339))
	log.Printf("=============================")

	// Remove the cached tree for this user
	delete(s.trees, req.UserName)

	log.Printf("Successfully purged replica for user %s", req.UserName)
	return &emptypb.Empty{}, nil
}

// TODO: compatible with the path search:
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
	nodes := make(map[string]*pb.MerkleNode)
	// Map to store parent-child relationships
	parentChildMap := make(map[string][]string)

	// Find the actual user root directory (should be exactly "/username")
	var rootUUID string

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

		// Create the MerkleNode (MerkleHash will be calculated later)
		node := &pb.MerkleNode{
			Uuid:       uuid,
			MerkleHash: "", // Will be calculated after children are set
			ParentUuid: parentUIDStr,
			FsEntry:    &pb.FSEntry{Metadata: metadataStruct},
		}

		// Store the node in the map
		nodes[uuid] = node

		// Track parent-child relationships
		if parentUID.Valid {
			parentChildMap[parentUID.String] = append(parentChildMap[parentUID.String], uuid)
		}

		// Check if this is the root directory by looking at the path
		if path == rootPath {
			rootUUID = uuid
		}
	}

	// Build the tree structure by setting children_uuids for each node
	for parentUUID, childUUIDs := range parentChildMap {
		if parent, exists := nodes[parentUUID]; exists {
			parent.ChildrenUuids = childUUIDs
		}
	}

	// If no root directory found, return error
	if rootUUID == "" {
		return nil, fmt.Errorf("user root directory not found: %s", rootPath)
	}

	// Create the MerkleTree with the nodes map
	tree := &pb.MerkleTree{
		RootUuid: rootUUID,
		Nodes:    nodes,
	}

	// Calculate MerkleHash for all nodes in the tree (bottom-up)
	calculateTreeMerkleHashes(tree)

	return tree, nil
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
	pb.RegisterFSTreeManagerServer(grpcServer, &server{
		db:    db,
		trees: make(map[string]*pb.MerkleTree),
	})

	log.Println("FS Tree Manager server starting on port 50052...")
	log.Println("Server ready to handle FetchReplica requests from database")

	// Start the server
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
