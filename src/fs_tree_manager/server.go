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
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
	_ "github.com/mattn/go-sqlite3"
	pb "github.com/puter/fs_tree_manager/go"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// LockedMerkleTree wraps a MerkleTree with its own lock
type LockedMerkleTree struct {
	tree *pb.MerkleTree
	lock sync.RWMutex
}

// Global variables for trees management
var (
	trees     map[int64]*LockedMerkleTree // In-memory tree cache by user_id
	treesLock sync.RWMutex                // Protects access to the trees map
)

type server struct {
	pb.UnimplementedFSTreeManagerServer
	db *sql.DB
}

// getMerkleTree atomically gets or creates a MerkleTree for a user
// Returns a read-locked tree that must be unlocked by the caller
func getMerkleTree(s *server, userID int64) (*LockedMerkleTree, error) {
	// First check if tree exists in cache (read lock)
	treesLock.RLock()
	lockedTree, exists := trees[userID]
	treesLock.RUnlock()

	if exists {
		// Tree exists, acquire read lock and return
		lockedTree.lock.RLock()
		return lockedTree, nil
	}

	// Tree doesn't exist, need to create it (write lock)
	treesLock.Lock()
	defer treesLock.Unlock()

	// Double-check after acquiring write lock
	if lockedTree, exists := trees[userID]; exists {
		lockedTree.lock.RLock()
		return lockedTree, nil
	}

	// Build new tree
	tree, err := s.buildUserFSTree(userID)
	if err != nil {
		return nil, err
	}

	// Create locked tree and store in cache
	lockedTree = &LockedMerkleTree{
		tree: tree,
	}
	trees[userID] = lockedTree

	// Acquire read lock and return
	lockedTree.lock.RLock()
	return lockedTree, nil
}

// getMerkleTreeForWrite atomically gets or creates a MerkleTree for a user for write operations
// Returns a write-locked tree that must be unlocked by the caller
func getMerkleTreeForWrite(s *server, userID int64) (*LockedMerkleTree, error) {
	// First check if tree exists in cache (read lock)
	treesLock.RLock()
	lockedTree, exists := trees[userID]
	treesLock.RUnlock()

	if exists {
		// Tree exists, acquire write lock and return
		lockedTree.lock.Lock()
		return lockedTree, nil
	}

	// Tree doesn't exist, need to create it (write lock)
	treesLock.Lock()
	defer treesLock.Unlock()

	// Double-check after acquiring write lock
	if lockedTree, exists := trees[userID]; exists {
		lockedTree.lock.Lock()
		return lockedTree, nil
	}

	// Build new tree
	tree, err := s.buildUserFSTree(userID)
	if err != nil {
		return nil, err
	}

	// Create locked tree and store in cache
	lockedTree = &LockedMerkleTree{
		tree: tree,
	}
	trees[userID] = lockedTree

	// Acquire write lock and return
	lockedTree.lock.Lock()
	return lockedTree, nil
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
// Note: This function assumes the tree is already locked by the caller
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
func (s *server) FetchReplica(ctx context.Context, req *pb.FetchReplicaRequest) (*pb.MerkleTree, error) {
	lockedTree, err := getMerkleTree(s, req.UserId)
	if err != nil {
		return nil, err
	}
	defer lockedTree.lock.RUnlock()

	log.Printf("[user %d] fetched replica", req.UserId)

	return lockedTree.tree, nil
}

// NewFSEntry implements the FSTreeManager service
func (s *server) NewFSEntry(ctx context.Context, req *pb.NewFSEntryRequest) (*emptypb.Empty, error) {
	userID := req.UserId
	fsEntry := req.FsEntry

	metadataMap := fsEntry.Metadata.AsMap()
	uid, ok := metadataMap["uid"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid metadata: missing uid")
	}

	parentUID, ok := metadataMap["parent_uid"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid metadata: missing parent_uid")
	}

	lockedTree, err := getMerkleTreeForWrite(s, userID)
	if err != nil {
		return nil, err
	}
	defer lockedTree.lock.Unlock()

	tree := lockedTree.tree
	parentNode, exists := tree.Nodes[parentUID]
	if !exists {
		return nil, fmt.Errorf("parent directory not found: %s", parentUID)
	}

	newNode := &pb.MerkleNode{
		Uuid:          uid,
		MerkleHash:    "",
		ParentUuid:    parentUID,
		FsEntry:       fsEntry,
		ChildrenUuids: []string{},
	}

	tree.Nodes[uid] = newNode

	parentNode.ChildrenUuids = append(parentNode.ChildrenUuids, uid)

	newNode.MerkleHash = calculateMerkleHash(newNode, []string{})

	recalculateAncestorHashes(tree, uid)

	return &emptypb.Empty{}, nil
}

// RemoveFSEntry implements the FSTreeManager service
func (s *server) RemoveFSEntry(ctx context.Context, req *pb.RemoveFSEntryRequest) (*emptypb.Empty, error) {
	userID := req.UserId
	uid := req.Uuid
	if uid == "" {
		return nil, fmt.Errorf("invalid request: missing uuid")
	}

	lockedTree, err := getMerkleTreeForWrite(s, userID)
	if err != nil {
		return nil, err
	}
	defer lockedTree.lock.Unlock()

	tree := lockedTree.tree
	targetNode, exists := tree.Nodes[uid]
	if !exists {
		return nil, fmt.Errorf("entry not found: %s", uid)
	}

	// Remove the node from its parent's children list
	if targetNode.ParentUuid != "" {
		if parentNode, parentExists := tree.Nodes[targetNode.ParentUuid]; parentExists {
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

	// Recalculate ancestor hashes
	if targetNode.ParentUuid != "" {
		recalculateAncestorHashes(tree, targetNode.ParentUuid)
	}

	log.Printf("[user %d] removed fs entry: %s", userID, uid)

	return &emptypb.Empty{}, nil
}

// PullDiff implements the FSTreeManager service
func (s *server) PullDiff(ctx context.Context, req *pb.PullRequest) (*pb.PushRequest, error) {
	lockedTree, err := getMerkleTree(s, req.UserId)
	if err != nil {
		return nil, fmt.Errorf("[user %d] no cached tree found: %v", req.UserId, err)
	}
	defer lockedTree.lock.RUnlock()

	tree := lockedTree.tree
	response := &pb.PushRequest{
		UserId:      req.UserId,
		PushRequest: []*pb.PushRequestItem{},
	}

	for _, pullRequestItem := range req.PullRequest {
		node, exists := tree.Nodes[pullRequestItem.Uuid]
		if !exists {
			log.Printf("[user %d] node not found: %s", req.UserId, pullRequestItem.Uuid)
			continue
		}

		// If hashes match, no need to send this node
		if node.MerkleHash == pullRequestItem.MerkleHash {
			log.Printf("[user %d] node %s merkle hash matches: %s", req.UserId, pullRequestItem.Uuid, node.MerkleHash)
			continue
		}

		log.Printf("[user %d] node %s merkle hash mismatch: %s != %s", req.UserId, pullRequestItem.Uuid, node.MerkleHash, pullRequestItem.MerkleHash)

		// Create push request item with node and its children
		pushItem := &pb.PushRequestItem{
			Uuid:       node.Uuid,
			MerkleHash: node.MerkleHash,
			FsEntry:    node.FsEntry,
			Children:   []*pb.PushRequestItem{},
		}

		// Add all children
		for _, childUUID := range node.ChildrenUuids {
			if childNode, childExists := tree.Nodes[childUUID]; childExists {
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
		log.Printf("[user %d] push request: %s, %d children", req.UserId, pushItem.Uuid, len(pushItem.Children))
	}

	return response, nil
}

// PurgeReplica implements the FSTreeManager service
func (s *server) PurgeReplica(ctx context.Context, req *pb.PurgeReplicaRequest) (*emptypb.Empty, error) {
	// Write lock for deleting from the trees map
	treesLock.Lock()
	delete(trees, req.UserId)
	treesLock.Unlock()

	return &emptypb.Empty{}, nil
}

const sqliteDBPath = "/var/puter/puter-database.sqlite"

const tableName = "fsentries"

// buildMetadata creates a comprehensive metadata structure matching the expected format
func buildMetadata(uuid, name, path, parentUID string, userID int64, isDir bool, size sql.NullInt64,
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
			"user_id": userID,
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
func (s *server) buildUserFSTree(userID int64) (*pb.MerkleTree, error) {
	rootPath := fmt.Sprintf("/%d", userID)

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

		metadataStruct, err := buildMetadata(uuid, name, path, parentUIDStr, userID, isDir, size,
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
	// Initialize global trees map
	trees = make(map[int64]*LockedMerkleTree)

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
		db: db,
	})

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
