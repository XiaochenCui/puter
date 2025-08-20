const merkle = async function(path) {
    if (!path) {
        throw new Error('Path is required');
    }
    
    const response = await fetch(`${this.APIOrigin}/fs/merkle?path=${encodeURIComponent(path)}`, {
        method: 'GET',
        headers: {
            'Authorization': `Bearer ${this.authToken}`,
            'Content-Type': 'application/json'
        }
    });
    
    if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error || 'Failed to get merkle tree');
    }

    const merkleTree = await response.json();

    window.clientMerkleTree = merkleTree;
    window.local_replica_available = true;

    return merkleTree;
};

export default merkle;
