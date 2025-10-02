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
func calculateMerkleHash(node *pb.MerkleNode, childrenHashes []string) string {
	hasher := xxhash.New()

	if node.FsEntry.Metadata != nil {
		metadataBytes, err := json.Marshal(node.FsEntry.Metadata.AsMap())
		if err == nil {
			hasher.Write(metadataBytes)
		}
	}

	sort.Strings(childrenHashes)

	for _, childHash := range childrenHashes {
		hasher.WriteString(childHash)
	}

	hash := hasher.Sum64()
	hashStr := fmt.Sprintf("%d", hash)
	return hashStr
}

// calculateTreeMerkleHashes calculates MerkleHash for all nodes in the tree (bottom-up)
func calculateTreeMerkleHashes(tree *pb.MerkleTree) {
	for _, node := range tree.Nodes {
		if len(node.ChildrenUuids) == 0 {
			node.MerkleHash = calculateMerkleHash(node, []string{})
		}
	}

	processed := make(map[string]bool)

	for {
		allProcessed := true
		for _, node := range tree.Nodes {
			if processed[node.Uuid] {
				continue
			}

			allChildrenProcessed := true
			childrenHashes := make([]string, 0, len(node.ChildrenUuids))
			for _, childID := range node.ChildrenUuids {
				if child, exists := tree.Nodes[childID]; exists {
					if !processed[childID] {
						allChildrenProcessed = false
						break
					}
					if child.MerkleHash != "" {
						childrenHashes = append(childrenHashes, child.MerkleHash)
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
	currentNodeID := nodeID

	for currentNodeID != "" {
		currentNode, exists := tree.Nodes[currentNodeID]
		if !exists {
			break
		}

		childrenHashes := make([]string, 0, len(currentNode.ChildrenUuids))
		for _, childID := range currentNode.ChildrenUuids {
			if child, exists := tree.Nodes[childID]; exists && child.MerkleHash != "" {
				childrenHashes = append(childrenHashes, child.MerkleHash)
			}
		}

		currentNode.MerkleHash = calculateMerkleHash(currentNode, childrenHashes)

		currentNodeID = currentNode.ParentUuid
	}
}

// FetchReplica implements the FSTreeManager service
func (s *server) FetchReplica(ctx context.Context, req *pb.UserName) (*pb.MerkleTree, error) {
	var tree *pb.MerkleTree
	var err error

	if cachedTree, exists := s.trees[req.UserName]; exists {
		tree = cachedTree
	} else {
		tree, err = s.buildUserFSTree(req.UserName)
		if err != nil {
			return nil, err
		}
		s.trees[req.UserName] = tree
	}

	log.Printf("[user %s] fetched replica", req.UserName)

	return tree, nil
}

// extractUsernameFromPath extracts the username from a path like /admin/Desktop/New -> admin
func extractUsernameFromPath(path string) (string, error) {
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
	metadataMap := req.Metadata.AsMap()
	path, ok := metadataMap["path"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid metadata: missing path information")
	}
	username, pathErr := extractUsernameFromPath(path)
	if pathErr != nil {
		return nil, fmt.Errorf("error extracting username from path: %v", pathErr)
	}

	var tree *pb.MerkleTree
	var err error
	if cachedTree, exists := s.trees[username]; exists {
		tree = cachedTree
	} else {
		tree, err = s.buildUserFSTree(username)
		if err != nil {
			return nil, err
		}
		s.trees[username] = tree
	}

	uid, ok := metadataMap["uid"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid metadata: missing uid")
	}

	parentUID, ok := metadataMap["parent_uid"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid metadata: missing parent_uid")
	}

	parentNode, exists := tree.Nodes[parentUID]
	if !exists {
		return nil, fmt.Errorf("parent directory not found: %s", parentUID)
	}

	newNode := &pb.MerkleNode{
		Uuid:          uid,
		MerkleHash:    "",
		ParentUuid:    parentUID,
		FsEntry:       req,
		ChildrenUuids: []string{},
	}

	tree.Nodes[uid] = newNode

	parentNode.ChildrenUuids = append(parentNode.ChildrenUuids, uid)

	newNode.MerkleHash = calculateMerkleHash(newNode, []string{})

	recalculateAncestorHashes(tree, uid)

	return &emptypb.Empty{}, nil
}

// RemoveFSEntry implements the FSTreeManager service
func (s *server) RemoveFSEntry(ctx context.Context, req *pb.FSEntry) (*emptypb.Empty, error) {
	metadataMap := req.Metadata.AsMap()
	path, ok := metadataMap["path"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid metadata: missing path information")
	}
	username, pathErr := extractUsernameFromPath(path)
	if pathErr != nil {
		return nil, fmt.Errorf("error extracting username from path: %v", pathErr)
	}

	tree, exists := s.trees[username]
	if !exists {
		return nil, fmt.Errorf("no cached tree found for user %s", username)
	}

	uid, ok := metadataMap["uid"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid metadata: missing uid")
	}

	node, exists := tree.Nodes[uid]
	if !exists {
		return nil, fmt.Errorf("entry not found: %s", uid)
	}

	if node.ParentUuid != "" {
		if parentNode, parentExists := tree.Nodes[node.ParentUuid]; parentExists {
			for i, childUUID := range parentNode.ChildrenUuids {
				if childUUID == uid {
					parentNode.ChildrenUuids = append(parentNode.ChildrenUuids[:i], parentNode.ChildrenUuids[i+1:]...)
					break
				}
			}
		}
	}

	delete(tree.Nodes, uid)

	if node.ParentUuid != "" {
		recalculateAncestorHashes(tree, node.ParentUuid)
	}

	return &emptypb.Empty{}, nil
}

// PullDiff implements the FSTreeManager service
func (s *server) PullDiff(ctx context.Context, req *pb.PullRequest) (*pb.PushRequest, error) {
	cachedTree, exists := s.trees[req.UserName]
	if !exists {
		return nil, fmt.Errorf("[user %s] no cached tree found", req.UserName)
	}

	response := &pb.PushRequest{
		UserName:    req.UserName,
		PushRequest: []*pb.PushRequestItem{},
	}

	for _, pullRequestItem := range req.PullRequest {
		node, exists := cachedTree.Nodes[pullRequestItem.Uuid]
		if !exists {
			log.Printf("[user %s] node not found: %s", req.UserName, pullRequestItem.Uuid)
			continue
		}

		// If hashes match, no need to send this node
		if node.MerkleHash == pullRequestItem.MerkleHash {
			log.Printf("[user %s] node %s merkle hash matches: %s", req.UserName, pullRequestItem.Uuid, node.MerkleHash)
			continue
		}

		log.Printf("[user %s] node %s merkle hash mismatch: %s != %s", req.UserName, pullRequestItem.Uuid, node.MerkleHash, pullRequestItem.MerkleHash)

		// Create push request item with node and its children
		pushItem := &pb.PushRequestItem{
			Uuid:       node.Uuid,
			MerkleHash: node.MerkleHash,
			FsEntry:    node.FsEntry,
			Children:   []*pb.PushRequestItem{},
		}

		// Add all children
		for _, childUUID := range node.ChildrenUuids {
			if childNode, childExists := cachedTree.Nodes[childUUID]; childExists {
				childPushItem := &pb.PushRequestItem{
					Uuid:       childNode.Uuid,
					MerkleHash: childNode.MerkleHash,
					FsEntry:    childNode.FsEntry,
					Children:   []*pb.PushRequestItem{},
				}
				pushItem.Children = append(pushItem.Children, childPushItem)
			}
		}

		response.PushRequest = append(response.PushRequest, pushItem)
	}

	return response, nil
}

// PurgeReplica implements the FSTreeManager service
func (s *server) PurgeReplica(ctx context.Context, req *pb.UserName) (*emptypb.Empty, error) {
	delete(s.trees, req.UserName)

	return &emptypb.Empty{}, nil
}

const sqliteDBPath = "/var/puter/puter-database.sqlite"

const tableName = "fsentries"

// buildMetadata creates a comprehensive metadata structure matching the expected format
func buildMetadata(uuid, name, path, parentUID, userName string, isDir bool, size sql.NullInt64,
	createdAt, modifiedAt, accessedAt float64, isPublic, isShortcut, isSymlink sql.NullBool,
	symlinkPath, sortBy, sortOrder sql.NullString, immutable sql.NullBool,
	metadata, associatedAppID, publicToken, fileRequestToken sql.NullString) (*structpb.Struct, error) {

	dirname := filepath.Dir(path)
	dirpath := dirname

	isEmpty := true
	if isDir {
		isEmpty = !size.Valid || size.Int64 == 0
	}

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
		"writable":  true,
		"parent_id": parentUID,
		"uid":       uuid,
	}

	return structpb.NewStruct(metadataMap)
}

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

	nodes := make(map[string]*pb.MerkleNode)
	parentChildMap := make(map[string][]string)

	var rootUUID string

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
			continue
		}

		parentUIDStr := ""
		if parentUID.Valid {
			parentUIDStr = parentUID.String
		}

		accessedAtValue := float64(time.Now().Unix())
		if accessedAt.Valid {
			accessedAtValue = accessedAt.Float64
		}

		metadataStruct, err := buildMetadata(uuid, name, path, parentUIDStr, userName, isDir, size,
			createdAt, modifiedAt, accessedAtValue, isPublic, isShortcut, isSymlink,
			symlinkPath, sortBy, sortOrder, immutable, metadata, associatedAppID, publicToken, fileRequestToken)
		if err != nil {
			continue
		}

		node := &pb.MerkleNode{
			Uuid:       uuid,
			MerkleHash: "",
			ParentUuid: parentUIDStr,
			FsEntry:    &pb.FSEntry{Metadata: metadataStruct},
		}

		nodes[uuid] = node

		if parentUID.Valid {
			parentChildMap[parentUID.String] = append(parentChildMap[parentUID.String], uuid)
		}

		if path == rootPath {
			rootUUID = uuid
		}
	}

	for parentUUID, childUUIDs := range parentChildMap {
		if parent, exists := nodes[parentUUID]; exists {
			parent.ChildrenUuids = childUUIDs
		}
	}

	if rootUUID == "" {
		return nil, fmt.Errorf("user root directory not found: %s", rootPath)
	}

	tree := &pb.MerkleTree{
		RootUuid: rootUUID,
		Nodes:    nodes,
	}

	calculateTreeMerkleHashes(tree)

	return tree, nil
}

func main() {
	db, err := sql.Open("sqlite3", sqliteDBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()

	pb.RegisterFSTreeManagerServer(grpcServer, &server{
		db:    db,
		trees: make(map[string]*pb.MerkleTree),
	})

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
