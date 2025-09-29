/*
 * Copyright (C) 2024-present Puter Technologies Inc.
 *
 * This file is part of Puter.
 *
 * Puter is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published
 * by the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 */


class FSTree {
    constructor(data) {
        if (!data) {
            throw new Error('FSTree requires valid data to initialize');
        }
        this.tree = data;
        this.nodes = data.nodes;
        this.rootId = data.root_id;
        
        // Get the root node to determine the root path
        const rootNode = this.nodes[this.rootId];
        if (rootNode && rootNode.fs_entry) {
            this.root = rootNode.fs_entry.path || "/";
        } else {
            this.root = "/";
        }
    }

    /**
     * Calculate Merkle hash for a node based on its metadata and children hashes
     * This matches the exact logic from server.go
     * @param {Object} node - The node to calculate hash for
     * @param {Array} childrenHashes - Array of child node hashes
     * @returns {string} - Hex string representation of the hash
     */
    calculateMerkleHash(node, childrenHashes = []) {
        // Create a hash object using our simple xxhash implementation
        let hasher = new Uint8Array(0);
        
        // Add self attributes to the hash (metadata as JSON)
        // This matches: hasher.Write(metadataBytes) in Go
        if (node.fs_entry) {
            const metadataBytes = new TextEncoder().encode(JSON.stringify(node.fs_entry));
            const combined = new Uint8Array(hasher.length + metadataBytes.length);
            combined.set(hasher);
            combined.set(metadataBytes, hasher.length);
            hasher = combined;
        }
        
        // Add children hashes in sorted order for consistency
        // This matches: sort.Strings(childrenHashes) and hasher.WriteString(childHash) in Go
        const sortedChildrenHashes = [...childrenHashes].sort();
        for (const childHash of sortedChildrenHashes) {
            const childBytes = new TextEncoder().encode(childHash);
            const combined = new Uint8Array(hasher.length + childBytes.length);
            combined.set(hasher);
            combined.set(childBytes, hasher.length);
            hasher = combined;
        }
    }

    /**
     * Recalculate Merkle hashes for all ancestors of a given node
     * @param {string} nodeId - The ID of the node whose ancestors need recalculation
     */
    recalculateAncestorHashes(nodeId) {
        const node = this.nodes[nodeId];
        if (!node) {
            return;
        }

        // Start from the current node and work up to the root
        let currentNodeId = nodeId;
        
        while (currentNodeId) {
            const currentNode = this.nodes[currentNodeId];
            if (!currentNode) {
                break;
            }

            // Get all children hashes
            const childrenHashes = [];
            if (currentNode.children_ids) {
                for (const childId of currentNode.children_ids) {
                    const childNode = this.nodes[childId];
                    if (childNode && childNode.merkle_hash) {
                        childrenHashes.push(childNode.merkle_hash);
                    }
                }
            }

            // Calculate new hash for current node
            currentNode.merkle_hash = this.calculateMerkleHash(currentNode, childrenHashes);

            // Move to parent
            currentNodeId = currentNode.parent_id;
        }
    }

    /**
     * Find a node by path in the tree
     * @param {string} path - Path to find (e.g., '/', '/folder', '/folder/file.txt')
     * @returns {Object|null} - Node object or null if not found
     */
    findNodeByPath(path) {
        // we're already in the root, so remove it
        path = path.replace(this.root, '');

        const parts = path.split('/').filter(part => part !== '');
        let currentId = this.rootId;

        for (const part of parts) {
            const currentNode = this.nodes[currentId];
            if (!currentNode || !currentNode.children_ids) {
                return null;
            }
            
            // Find child with matching name
            const foundId = currentNode.children_ids.find(childId => {
                const childNode = this.nodes[childId];
                return childNode && childNode.fs_entry && childNode.fs_entry.name === part;
            });
            
            if (!foundId) {
                return null;
            }
            currentId = foundId;
        }

        return this.nodes[currentId];
    }

    /**
     * Find a node by UUID in the tree
     * @param {string} uid - UUID to find
     * @returns {Object|null} - Node object or null if not found
     */
    findNodeByUUID(uid) {
        // Direct lookup in nodes map
        return this.nodes[uid] || null;
    }

    /**
     * Read directory contents.
     * 
     * @param {Object} options - Options object
     * @param {string} [options.path] - Path to read directory for
     * @param {string} [options.uid] - UUID to read directory for
     * @returns {Array} - Array of child fs_entry objects
     */
    readdir(options) {
        const path = options.path;
        const uid = options.uid;
        let node = null;

        if (uid) {
            node = this.findNodeByUUID(uid);
        } else if (path) {
            node = this.findNodeByPath(path);
        } else {
            throw new Error('Either path or uid must be provided');
        }

        if (!node) {
            throw new Error(`Path not found: ${path}`);
        }

        if (!node.fs_entry?.is_dir) {
            throw new Error(`Not a directory: ${path}`);
        }

        // Get children by their IDs
        const childrenIds = node.children_ids || [];
        return childrenIds
            .map(childId => this.nodes[childId])
            .filter(childNode => childNode && childNode.fs_entry)
            .map(childNode => childNode.fs_entry);
    }

    /**
     * Get node fs_entry
     * @param {Object} options - Options object
     * @param {string} [options.path] - Path to get fs_entry for
     * @param {string} [options.uid] - UUID to get fs_entry for
     * @returns {Object|null} - fs_entry object or null if not found
     */
    stat(options) {
        const path = options.path;
        const uid = options.uid;
        let node = null;

        if (uid) {
            node = this.findNodeByUUID(uid);
        } else if (path) {
            node = this.findNodeByPath(path);
        } else {
            throw new Error('Either path or uid must be provided');
        }

        return node?.fs_entry;
    }

    /**
     * Add a new directory to the tree
     * @param {Object} fs_entry - The fs_entry object of the new directory
     */
    newDirectory(fs_entry) {
        if (!fs_entry || !fs_entry.uid) {
            throw new Error('Invalid fs_entry: must have uid');
        }

        if (!fs_entry.is_dir) {
            throw new Error('fs_entry must be a directory');
        }

        // Find the parent directory by uid
        const parentNode = this.findNodeByUUID(fs_entry.parent_uid);
        if (!parentNode) {
            throw new Error(`Parent directory not found: ${fs_entry.parent_uid}`);
        }

        // Create new directory node
        const newNode = {
            id: fs_entry.uid,
            merkle_hash: '', // Will be calculated below
            parent_id: fs_entry.parent_uid,
            fs_entry: fs_entry,
            children_ids: []
        };

        // Add to nodes map
        this.nodes[fs_entry.uid] = newNode;

        // Add to parent's children_ids
        if (!parentNode.children_ids) {
            parentNode.children_ids = [];
        }
        parentNode.children_ids.push(fs_entry.uid);

        // Calculate Merkle hash for the new directory (empty children)
        newNode.merkle_hash = this.calculateMerkleHash(newNode, []);

        // Recalculate Merkle hashes for all ancestors
        this.recalculateAncestorHashes(fs_entry.uid);
    }
}

export default FSTree;
