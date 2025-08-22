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
        if (!data || !data.data) {
            throw new Error('FSTree requires valid data to initialize');
        }
        this.tree = data.data;
        this.root = "/" + data.data.name;
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
        let current = this.tree;

        for (const part of parts) {
            if (!current.children) {
                return null;
            }
            
            const found = current.children.find(child => child.name === part);
            if (!found) {
                return null;
            }
            current = found;
        }

        return current;
    }

    /**
     * Find a node by UUID in the tree
     * @param {string} uid - UUID to find
     * @returns {Object|null} - Node object or null if not found
     */
    findNodeByUUID(uid) {
        const searchInNode = (node) => {
            // Check if current node has the UUID
            if (node.metadata?.uid === uid) {
                return node;
            }

            // Search in children recursively
            if (node.children) {
                for (const child of node.children) {
                    const found = searchInNode(child);
                    if (found) {
                        return found;
                    }
                }
            }

            return null;
        };

        return searchInNode(this.tree);
    }

    /**
     * Read directory contents.
     * 
     * @param {Object} options - Options object
     * @param {string} [options.path] - Path to read directory for
     * @param {string} [options.uid] - UUID to read directory for
     * @returns {Array} - Array of child nodes
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

        if (!node.metadata?.is_dir) {
            throw new Error(`Not a directory: ${path}`);
        }

        return node.children.map(child => child.metadata);
    }

    /**
     * Get node metadata
     * @param {Object} options - Options object
     * @param {string} [options.path] - Path to get metadata for
     * @param {string} [options.uid] - UUID to get metadata for
     * @returns {Object|null} - Metadata object or null if not found
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

        return node?.metadata;
    }

}

export default FSTree;
