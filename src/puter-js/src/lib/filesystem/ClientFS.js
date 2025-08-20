
/**
 * Simple client-side filesystem implementation
 * Completely relies on the global merkle tree for POC
 * Only implements stat and readdir APIs
 */
// Global merkle tree for POC - stores the filesystem structure
window.clientMerkleTree = null;

export class ClientFS {
    constructor() {
        // No initialization needed - everything comes from the global merkle tree
    }

    /**
     * Get filesystem statistics
     * @param {Object} options - Stat options
     * @param {string} options.path - Path to stat
     * @param {string} options.uid - UUID to stat
     * @returns {Promise<Object>} Node statistics
     */
    async stat(options) {
        const { path, uid } = options;
        const identifier = path || uid;
        
        if (!identifier) {
            throw new Error('Either path or uid must be provided');
        }

        // Check if we have a global merkle tree
        if (!window.clientMerkleTree) {
            throw new Error('No merkle tree available');
        }

        // Find the node in the merkle tree
        const node = this._findNodeInTree(window.clientMerkleTree, identifier);
        if (!node) {
            throw new Error(`Node not found: ${identifier}`);
        }

        // Return a stat object compatible with puter.fs.stat
        return {
            uid: node.hash, // Use hash as uid
            name: node.path.split('/').pop() || node.path,
            path: node.path,
            is_dir: node.stat.type === 'directory',
            size: node.stat.size || 0,
            created_at: node.stat.created ? new Date(node.stat.created).toISOString() : new Date().toISOString(),
            updated_at: node.stat.modified ? new Date(node.stat.modified).toISOString() : new Date().toISOString(),
            parent: node.path === '/' ? null : node.path.substring(0, node.path.lastIndexOf('/')) || '/'
        };
    }

    /**
     * Read directory contents
     * @param {Object} options - Readdir options
     * @param {string} options.path - Path to read
     * @returns {Promise<Array>} Array of child nodes
     */
    async readdir(options) {
        const { path } = options;
        
        if (!path) {
            throw new Error('Path must be provided');
        }

        // Check if we have a global merkle tree
        if (!window.clientMerkleTree) {
            throw new Error('No merkle tree available');
        }

        const node = this._findNodeInTree(window.clientMerkleTree, path);
        if (!node) {
            throw new Error(`Directory not found: ${path}`);
        }

        if (node.stat.type !== 'directory') {
            throw new Error(`Not a directory: ${path}`);
        }

        // Return array of child nodes from the merkle tree
        if (!node.children || !Array.isArray(node.children)) {
            return [];
        }

        return node.children.map(child => ({
            uid: child.hash, // Use hash as uid
            name: child.path.split('/').pop() || child.path,
            path: child.path,
            is_dir: child.stat.type === 'directory',
            size: child.stat.size || 0,
            created_at: child.stat.created ? new Date(child.stat.created).toISOString() : new Date().toISOString(),
            updated_at: child.stat.modified ? new Date(child.stat.modified).toISOString() : new Date().toISOString(),
            parent: path
        }));
    }

    /**
     * Helper method to find a node in the merkle tree
     * @param {Object} tree - The merkle tree to search
     * @param {string} targetPath - The path to find
     * @returns {Object|null} The found node or null
     */
    _findNodeInTree(tree, targetPath) {
        if (tree.path === targetPath) {
            return tree;
        }
        if (tree.children && Array.isArray(tree.children)) {
            for (const child of tree.children) {
                const found = this._findNodeInTree(child, targetPath);
                if (found) return found;
            }
        }
        return null;
    }
}
