package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
	_ "github.com/mattn/go-sqlite3"
	pb "github.com/puter/fs_tree_manager/go"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type (
	merkleTree struct {
		tree *pb.MerkleTree
		lock sync.RWMutex

		// Last time the tree was synced from database.
		lastSynced time.Time

		// Last time the tree was read (by FetchReplica/PullDiff).
		lastRead time.Time
	}

	server struct {
		pb.UnimplementedFSTreeManagerServer
		db *sql.DB
	}
)

var (
	// In-memory tree cache by user_id.
	trees map[int64]*merkleTree

	// The global lock.
	treesLock sync.RWMutex

	// Memory threshold in bytes (2GB)
	memoryThresholdBytes int64 = 2 * 1024 * 1024 * 1024

	// Make FS-Tree Manager unstable and laggy.
	chaos = false

	debug = false
)

func newMerkleTree(tree *pb.MerkleTree) *merkleTree {
	return &merkleTree{
		tree:       tree,
		lastSynced: time.Now(),
	}
}

// checkMemoryUsage checks if the current memory usage exceeds the threshold
func checkMemoryUsage() error {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	if m.Alloc > uint64(memoryThresholdBytes) {
		return fmt.Errorf("memory usage (%d bytes) exceeds threshold (%d bytes)", m.Alloc, memoryThresholdBytes)
	}

	return nil
}

// getMerkleTree atomically gets or creates a MerkleTree for a user
// Returns a read-locked tree that must be unlocked by the caller
func getMerkleTree(s *server, userID int64) (*merkleTree, error) {
	treesLock.RLock()
	lockedTree, exists := trees[userID]
	treesLock.RUnlock()

	if exists {
		lockedTree.lock.RLock()
		return lockedTree, nil
	}

	treesLock.Lock()
	defer treesLock.Unlock()

	if err := checkMemoryUsage(); err != nil {
		return nil, err
	}

	tree, err := s.buildUserFSTree(userID)
	if err != nil {
		return nil, err
	}

	lockedTree = newMerkleTree(tree)
	trees[userID] = lockedTree

	lockedTree.lock.RLock()
	return lockedTree, nil
}

// getMerkleTreeForWrite gets a MerkleTree for a user for write operations
// Only operates on trees that exist in memory
// Returns a write-locked tree that must be unlocked by the caller
func getMerkleTreeForWrite(s *server, userID int64) (*merkleTree, error) {
	treesLock.RLock()
	lockedTree, exists := trees[userID]
	treesLock.RUnlock()

	if !exists {
		return nil, fmt.Errorf("tree for user %d does not exist in memory", userID)
	}

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
	if chaos {
		time.Sleep(10 * time.Second)
	}

	lockedTree, err := getMerkleTree(s, req.UserId)
	if err != nil {
		return nil, err
	}
	defer lockedTree.lock.RUnlock()

	// Update lastRead timestamp
	lockedTree.lastRead = time.Now()

	return lockedTree.tree, nil
}

// NewFSEntry implements the FSTreeManager service
func (s *server) NewFSEntry(ctx context.Context, req *pb.NewFSEntryRequest) (*emptypb.Empty, error) {
	if chaos {
		if err := mayCrash(); err != nil {
			return nil, err
		}
	}

	userID := req.UserId
	fsEntry := req.FsEntry

	metadataMap := fsEntry.Metadata.AsMap()
	uid, ok := metadataMap["uid"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid metadata: missing uid")
	}

	lockedTree, err := getMerkleTreeForWrite(s, userID)
	if err != nil {
		return nil, err
	}
	defer lockedTree.lock.Unlock()

	parentUUID, err := getParentUUID(metadataMap, lockedTree.tree.Nodes)
	if err != nil {
		return nil, err
	}

	tree := lockedTree.tree
	parentNode, exists := tree.Nodes[parentUUID]
	if !exists {
		return nil, fmt.Errorf("parent directory not found: %s", parentUUID)
	}

	newNode := &pb.MerkleNode{
		Uuid:          uid,
		MerkleHash:    "",
		ParentUuid:    parentUUID,
		FsEntry:       fsEntry,
		ChildrenUuids: []string{},
	}

	tree.Nodes[uid] = newNode

	parentNode.ChildrenUuids = append(parentNode.ChildrenUuids, uid)

	newNode.MerkleHash = calculateMerkleHash(newNode, []string{})

	recalculateAncestorHashes(tree, uid)

	if debug {
		parentPath := parentNode.FsEntry.Metadata.AsMap()["path"].(string)
		parentUUID = parentNode.Uuid
		log.Printf("[user %d] new fs entry, (path: %s, uuid: %s), (parent_path: %s, parent_uuid: %s)", userID, metadataMap["path"], uid, parentPath, parentUUID)
		integrityCheck()
	}

	return &emptypb.Empty{}, nil
}

// TODO: remove this once parent_path is always consistent with parent_uuid
func getParentUUID(metadata map[string]any, nodes map[string]*pb.MerkleNode) (UUID string, err error) {
	// Check the inconsistency between "parent_path" and "parent_uuid", the inconsistency
	// occurs in several scenarios:
	// - When moving a directory from ~/Desktop to ~/trash, the parent_uuid is not updated.

	// parent_path comes from "dirpath" field
	parentPath := metadata["dirpath"].(string)

	// parent_uuid comes from "parent_uid"/"parent_id" field, just use parent_uid here.
	parentUUID := metadata["parent_uid"].(string)

	if parentUUID == "" {
		return "", fmt.Errorf("parent_uuid is empty")
	}

	parentNode, parentExists := nodes[parentUUID]
	if !parentExists {
		return "", fmt.Errorf("parent node not found, uuid: %s", parentUUID)
	}

	pathFromUUID := parentNode.FsEntry.Metadata.AsMap()["path"].(string)
	if parentPath != pathFromUUID {
		// When missmatch happens, use parentPath.
		log.Printf("parent_path(preferred) and parent_uuid mismatch, parent_path: %s, pathFromUUID: %s, uuid: %s", parentPath, pathFromUUID, parentUUID)
		return pathToUUID(parentPath, nodes)
	}

	return parentUUID, nil
}

func pathToUUID(path string, nodes map[string]*pb.MerkleNode) (UUID string, err error) {
	// TODO: optimize this by using a trie tree. Currently we cannot traverse the tree
	// using path.
	for _, node := range nodes {
		if node.FsEntry.Metadata.AsMap()["path"].(string) == path {
			return node.Uuid, nil
		}
	}
	return "", fmt.Errorf("node not found, path: %s", path)
}

// RemoveFSEntry implements the FSTreeManager service
func (s *server) RemoveFSEntry(ctx context.Context, req *pb.RemoveFSEntryRequest) (*emptypb.Empty, error) {
	if chaos {
		if err := mayCrash(); err != nil {
			return nil, err
		}
	}

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

	if debug {
		parentPath := targetNode.FsEntry.Metadata.AsMap()["path"].(string)
		parentUUID := targetNode.ParentUuid
		log.Printf("[user %d] removed fs entry, (path: %s, uuid: %s), (parent_path: %s, parent_uuid: %s)", userID, targetNode.FsEntry.Metadata.AsMap()["path"], uid, parentPath, parentUUID)
		integrityCheck()
	}

	return &emptypb.Empty{}, nil
}

func (s *server) PullDiff(ctx context.Context, req *pb.PullRequest) (*pb.PushRequest, error) {
	if chaos {
		if err := mayCrash(); err != nil {
			return nil, err
		}
	}

	lockedTree, err := getMerkleTree(s, req.UserId)
	if err != nil {
		return nil, fmt.Errorf("[user %d] no cached tree found: %v", req.UserId, err)
	}
	defer lockedTree.lock.RUnlock()

	// Update lastRead timestamp
	lockedTree.lastRead = time.Now()

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

		// If hashes match, no need to send this node.
		if node.MerkleHash == pullRequestItem.MerkleHash {
			continue
		}

		// Create push request item with node and its children.
		pushItem := &pb.PushRequestItem{
			Uuid:       node.Uuid,
			MerkleHash: node.MerkleHash,
			FsEntry:    node.FsEntry,
			Children:   []*pb.PushRequestItem{},
		}

		// Add all children.
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
	}

	return response, nil
}

func (s *server) PurgeReplica(ctx context.Context, req *pb.PurgeReplicaRequest) (*emptypb.Empty, error) {
	treesLock.Lock()
	delete(trees, req.UserId)
	treesLock.Unlock()

	return &emptypb.Empty{}, nil
}

func mayCrash() error {
	v := rand.Intn(100)
	if v < 10 {
		panic("crash")
	} else if v < 30 {
		time.Sleep(10 * time.Second)
	} else if v < 60 {
		return fmt.Errorf("error")
	}
	return nil
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
	query := `
		SELECT uuid, name, is_dir, size, created, modified, path, parent_uid, 
		       is_public, is_shortcut, is_symlink, symlink_path, sort_by, sort_order,
		       immutable, metadata, accessed, associated_app_id, public_token, file_request_token
		FROM fsentries 
		WHERE user_id = ?
	`

	rows, err := s.db.Query(query, userID)
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

		if strings.Count(path, "/") == 1 {
			rootUUID = uuid
		}
	}

	for parentUUID, childUUIDs := range parentChildMap {
		if parent, exists := nodes[parentUUID]; exists {
			parent.ChildrenUuids = childUUIDs
		}
	}

	if rootUUID == "" {
		return nil, fmt.Errorf("[user %d] root directory not found", userID)
	}

	tree := &pb.MerkleTree{
		RootUuid: rootUUID,
		Nodes:    nodes,
	}

	calculateTreeMerkleHashes(tree)

	return tree, nil
}

func integrityCheck() {
	treesLock.RLock()
	defer treesLock.RUnlock()

	for userID, warppedTree := range trees {
		tree := warppedTree.tree

		root, exists := tree.Nodes[tree.RootUuid]
		if !exists {
			log.Fatalf("[user %d] root uuid not found: %s", userID, tree.RootUuid)
		}
		rootPath := root.FsEntry.Metadata.AsMap()["path"].(string)

		for UUID, node := range tree.Nodes {
			// check: uuid is consistent
			if UUID != node.Uuid {
				log.Fatalf("[user %d] uuid is inconsistent: %s != %s", userID, UUID, node.Uuid)
			}

			// check with parent
			if node.Uuid != tree.RootUuid {
				// check: all node should have a parent
				if node.ParentUuid == "" {
					log.Fatalf("[user %d] parent uuid is empty: %s", userID, node.Uuid)
				}

				// check: parent uuid is valid
				parent, exists := tree.Nodes[node.ParentUuid]
				if !exists {
					log.Fatalf("[user %d] parent uuid not found: %s", userID, node.ParentUuid)
				}

				// check: parent has self as a child
				if !slices.Contains(parent.ChildrenUuids, node.Uuid) {
					log.Fatalf("[user %d] parent has self as a child: %s", userID, node.Uuid)
				}

				// check: parent path is a prefix
				parentPath := parent.FsEntry.Metadata.AsMap()["path"].(string)
				if !strings.HasPrefix(parentPath, rootPath) {
					log.Fatalf("[user %d] parent path is not a prefix: %s", userID, parentPath)
				}
			}

			// check with children
			for _, childUUID := range node.ChildrenUuids {
				// check: child uuid is valid
				if _, exists := tree.Nodes[childUUID]; !exists {
					log.Fatalf("[user %d] child uuid not found: %s", userID, childUUID)
				}
			}
		}
	}
}

// purgeOldTrees removes trees that haven't been read in 1 minute or synced in 5 minutes
func purgeOldTrees() {
	treesLock.Lock()
	defer treesLock.Unlock()

	readCutoff := time.Now().Add(-1 * time.Minute)
	syncCutoff := time.Now().Add(-5 * time.Minute)
	var toDelete []int64

	for userID, lockedTree := range trees {
		// Purge if either lastRead is older than 1 minute OR lastSynced is older than 5 minutes
		if lockedTree.lastRead.Before(readCutoff) || lockedTree.lastSynced.Before(syncCutoff) {
			toDelete = append(toDelete, userID)
		}
	}

	for _, userID := range toDelete {
		delete(trees, userID)
	}
	log.Printf("purged %d old trees, %d trees remaining", len(toDelete), len(trees))
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// Initialize global trees map
	trees = make(map[int64]*merkleTree)

	// Start background job to purge old trees every 30 seconds
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			purgeOldTrees()
		}
	}()

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
