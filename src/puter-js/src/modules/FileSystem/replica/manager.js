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

import io from '../../../lib/socket.io/socket.io.esm.min.js';
import FSTree from './tree.js';

class ReplicaManager {
    constructor() {
        this.socket = null;
        this.isConnected = false;
        this.isInitialized = false;
        this.username = null;
        this.hashSenderInterval = null;

        this.available = false;
    }

    /**
     * Initialize the replica manager with context
     */
    async initialize(context) {
        if ( this.isInitialized ) {
            return;
        }

        this.authToken = context.authToken;
        this.APIOrigin = context.APIOrigin;
        this.appID = context.appID;

        // Fetch username from whoami endpoint if not provided in context
        if ( !context.username ) {
            this.username = await this.fetchUsername();
        } else {
            this.username = context.username;
        }

        this.isInitialized = true;

        this.connect();
    }

    /**
     * Fetch username from whoami endpoint using direct API call
     */
    async fetchUsername() {
        try {
            const resp = await fetch(this.APIOrigin + '/whoami', {
                headers: {
                    Authorization: `Bearer ${this.authToken}`,
                },
            });

            const result = await resp.json();
            return result.username;
        } catch( error ) {
            console.error('Replica Manager: Failed to fetch username from whoami endpoint:', error);
            throw error;
        }
    }

    /**
     * Connect to the websocket
     */
    connect() {
        if ( this.socket ) {
            this.socket.disconnect();
        }

        this.socket = io(this.APIOrigin, {
            auth: {
                auth_token: this.authToken,
            },
        });

        this.bindEvents();
    }

    /**
     * Bind websocket events
     */
    bindEvents() {
        this.socket.on('connect', () => {
            this.isConnected = true;
            if ( puter.debugMode ) {
                console.log('Replica Manager: Connected', this.socket.id);
            }

            // Automatically fetch user's root path on connection
            this.fetchUserRoot();

            // Start background task to pull diff
            this.startPullDiff();
        });

        this.socket.on('disconnect', () => {
            this.isConnected = false;
            if ( puter.debugMode ) {
                console.log('Replica Manager: Disconnected');
            }
        });

        this.socket.on('reconnect', (_attempt) => {
            this.isConnected = true;
            if ( puter.debugMode ) {
                console.log('Replica Manager: Reconnected', this.socket.id);
            }

            // Refetch user's root path on reconnection
            this.fetchUserRoot();

            // Restart pull diff on reconnection
            this.startPullDiff();
        });

        this.socket.on('reconnect_attempt', (_attempt) => {
            if ( puter.debugMode ) {
                console.log('Replica Manager: Reconnection Attempt', _attempt);
            }
        });

        this.socket.on('reconnect_error', (error) => {
            if ( puter.debugMode ) {
                console.log('Replica Manager: Reconnection Error', error);
            }
        });

        this.socket.on('reconnect_failed', () => {
            if ( puter.debugMode ) {
                console.log('Replica Manager: Reconnection Failed');
            }
        });

        this.socket.on('error', (error) => {
            if ( puter.debugMode ) {
                console.error('Replica Manager Error:', error);
            }
        });

        this.socket.on('replica/fetch/success', (data) => {
            this.handleReplicaSuccess(data);
        });

        this.socket.on('replica/fetch/error', (data) => {
            this.handleReplicaError(data);
        });

        this.socket.on('replica/pull_diff/success', (data) => {
            this.handlePushRequest(data);

            {
                const debug_node_123 = this.FSTree.findNodeByPath('/admin/Desktop');
                console.log(`debug: found node: ${debug_node_123.fs_entry.path} (uuid: ${debug_node_123.uuid}), children: ${debug_node_123.children_uuids}`);
            }
        });

        this.socket.on('replica/pull_diff/error', (data) => {
            this.handlePullDiffError(data);
        });
    }

    /**
     * Fetch the user's root path
     */
    fetchUserRoot() {
        if ( !this.username ) {
            console.warn('Replica Manager: No username available for fetching root');
            return;
        }

        const userRootPath = `/${this.username}`;

        this.socket.emit('replica/fetch', {
            path: userRootPath,
            requestId: 'user_root', // Special request ID for user root
        });
    }

    /**
     * Handle successful replica fetch
     */
    handleReplicaSuccess(data) {
        // Initialize the FSTree
        this.FSTree = new FSTree(data.data);
        this.available = true;

        console.log('client-replica initialized');
    }

    handleReplicaError(data) {
        console.error('replica manager: failed to fetch replica:', data);
        this.available = false;
    }

    handlePullDiffError(data) {
        console.error('replica manager: failed to pull diff:', data);
        this.available = false;
    }

    /**
     * Handle push request
     */
    handlePushRequest(data) {
        const pushRequest = data?.data?.push_request;

        if ( !this.available || !pushRequest ) {
            return;
        }

        const nextPullRequest = [];

        if ( pushRequest.length > 0 ) {
            console.log('push request:', pushRequest);
        } else {
            console.log('push request: no push request');
        }

        for ( const pushItem of pushRequest ) {
            console.log(`remote fsentry: ${pushItem.fs_entry.path} (merkle_hash: ${pushItem.merkle_hash})`);

            // process level-1 node
            const node = this.FSTree.nodes[pushItem.uuid];
            if ( node ) {
                // update existing
                node.fs_entry = pushItem.fs_entry;
                node.merkle_hash = pushItem.merkle_hash;
            } else {
                // new fsentry on remote, add it and fetch its children
                this.addNode(pushItem);

                // Add to parent's children
                if ( pushItem.fs_entry.parent_uid ) {
                    const parentNode = this.FSTree.nodes[pushItem.fs_entry.parent_uid];
                    if ( parentNode ) {
                        if ( !parentNode.children_uuids ) {
                            parentNode.children_uuids = [];
                        }
                        parentNode.children_uuids.push(pushItem.uuid);
                    } else {
                        console.error(`parent node not found: ${pushItem.fs_entry.parent_uid}`);
                    }
                } else {
                    console.error(`parent node not found: ${pushItem.fs_entry.id}`);
                }

                nextPullRequest.push({
                    uuid: pushItem.uuid,
                    // use empty hash to force-fetch its children
                    merkle_hash: '',
                });
                continue;
            }

            // process children
            if ( pushItem.children ) {
                const localChildren = node ? (node.children_uuids || []) : [];
                const serverChildren = pushItem.children.map(child => child.uuid);

                // fsentry removed from server, remove it in local as well
                console.log(`local children: ${localChildren}`);
                console.log(`server children: ${serverChildren}`);
                for ( const localChildId of localChildren ) {
                    if ( !serverChildren.includes(localChildId) ) {
                        this.removeNodeAndDescendants(localChildId);
                    }

                    {
                        const debug_node_123 = this.FSTree.findNodeByPath('/admin/Desktop');
                        console.log(`debug-3: found node: ${debug_node_123.fs_entry.path} (uuid: ${debug_node_123.uuid}), children: ${debug_node_123.children_uuids}`);
                    }
                }

                {
                    const debug_node_123 = this.FSTree.findNodeByPath('/admin/Desktop');
                    console.log(`debug-2: found node: ${debug_node_123.fs_entry.path} (uuid: ${debug_node_123.uuid}), children: ${debug_node_123.children_uuids}`);
                }

                for ( const child of pushItem.children ) {
                    const localChild = this.FSTree.nodes[child.uuid];

                    if ( localChild ) {
                        console.log(`remote fsentry (level_2): ${child.fs_entry.path} (merkle_hash: ${child.merkle_hash}, uuid: ${child.uuid})`);
                        if ( child.fs_entry.path.includes('Desktop') ) {
                            console.log(`local fsentry (level_2): ${localChild.fs_entry.path} (merkle_hash: ${localChild.merkle_hash})`);
                        }
                    }

                    if ( !localChild ) {
                        // new fsentry on remote, add it and fetch its children
                        this.addNode(child);

                        // Add to parent's children
                        const parentNode = this.FSTree.nodes[pushItem.fs_entry.id];
                        if ( parentNode ) {
                            if ( !parentNode.children_uuids ) {
                                parentNode.children_uuids = [];
                            }
                            parentNode.children_uuids.push(child.uuid);
                        } else {
                            console.error(`parent node not found: ${pushItem.fs_entry.id}`);
                        }

                        nextPullRequest.push({
                            uuid: child.uuid,
                            // use empty hash to force-fetch its children
                            merkle_hash: '',
                        });
                    } else if ( localChild.merkle_hash !== child.merkle_hash ) {
                        // fsentry updated on remote, update and fetch its children
                        localChild.fs_entry = child.fs_entry;
                        localChild.merkle_hash = child.merkle_hash;
                        nextPullRequest.push({
                            uuid: child.uuid,
                            // use empty hash to force-fetch its children
                            merkle_hash: '',
                        });
                    }
                }
            }
        }

        // Send next pull request if there are nodes to update
        if ( nextPullRequest.length > 0 ) {
            this.socket.emit('replica/pull_diff', {
                user_name: this.username,
                pull_request: nextPullRequest,
            });
            console.log(`sent continuous pull request: ${JSON.stringify(nextPullRequest)}`);
        }
    }

    /**
     * Add a new node to the tree
     */
    addNode(nodeData) {
        const newNode = {
            uuid: nodeData.uuid,
            merkle_hash: nodeData.merkle_hash,
            parent_uuid: nodeData.fs_entry.parent_uid,
            fs_entry: nodeData.fs_entry,
            children_uuids: [],
        };

        this.FSTree.nodes[nodeData.uuid] = newNode;

        // // Add to parent's children
        // if ( nodeData.fs_entry.parent_uid ) {
        //     const parentNode = this.FSTree.nodes[nodeData.fs_entry.parent_uid];
        //     if ( parentNode ) {
        //         if ( !parentNode.children_uuids ) {
        //             parentNode.children_uuids = [];
        //         }
        //         parentNode.children_uuids.push(nodeData.uuid);
        //     }
        // }
    }

    /**
     * Remove a node and all its descendants from the local replica
     */
    removeNodeAndDescendants(nodeId) {
        const node = this.FSTree.nodes[nodeId];
        if ( !node ) {
            return;
        }

        {
            const parentNode = this.FSTree.nodes[node.parent_uuid];
            console.log(`before remove, parent: ${parentNode.fs_entry.path} (uuid: ${parentNode.uuid}), children: ${parentNode.children_uuids}`);
        }

        // Remove from parent's children
        if ( node.parent_uuid ) {
            const parentNode = this.FSTree.nodes[node.parent_uuid];
            if ( parentNode && parentNode.children_uuids ) {
                const index = parentNode.children_uuids.indexOf(nodeId);
                if ( index > -1 ) {
                    parentNode.children_uuids.splice(index, 1);
                }
            }
        }

        {
            const parentNode = this.FSTree.nodes[node.parent_uuid];
            console.log(`after remove, parent: ${parentNode.fs_entry.path} (uuid: ${parentNode.uuid}), children: ${parentNode.children_uuids}`);
        }

        {
            const debug_node_123 = this.FSTree.findNodeByPath('/admin/Desktop');
            console.log(`debug: found node: ${debug_node_123.fs_entry.path} (uuid: ${debug_node_123.uuid}), children: ${debug_node_123.children_uuids}`);
        }

        // Remove all children recursively
        if ( node.children_uuids ) {
            for ( const childId of node.children_uuids ) {
                this.removeNodeAndDescendants(childId);
            }
        }

        // Remove the node itself
        delete this.FSTree.nodes[nodeId];
    }

    /**
     * Get the current socket instance
     */
    getSocket() {
        return this.socket;
    }

    /**
     * Check if connected
     */
    isSocketConnected() {
        return this.isConnected && this.socket && !this.socket.disconnected;
    }

    startPullDiff() {
        // Clear any existing interval
        if ( this.pullDiffInterval ) {
            clearInterval(this.pullDiffInterval);
        }

        // Set up interval to send hash every 5 seconds
        this.pullDiffInterval = setInterval(() => {
            this.pullDiff();
        }, 5000);

        // // debug
        // this.debugInterval = setInterval(() => {
        //     const node = this.FSTree.findNodeByPath('/admin/Desktop');
        //     console.log(`readdir: found node: ${node.fs_entry.path} (uuid: ${node.uuid}), children: ${node.children_uuids}`);
        // }, 500);
    }

    pullDiff() {
        if ( !this.isSocketConnected() || !this.FSTree ) {
            this.stopPullDiff();
            return;
        }

        try {
            const rootNode = this.FSTree.nodes[this.FSTree.rootId];
            if ( rootNode && rootNode.merkle_hash ) {
                // Create PullRequest format according to proto definition
                const pullRequest = {
                    user_name: this.username,
                    pull_request: [
                        {
                            uuid: rootNode.uuid,
                            merkle_hash: rootNode.merkle_hash,
                        },
                    ],
                };

                this.socket.emit('replica/pull_diff', pullRequest);
            }
        } catch( error ) {
            console.error('error in pullDiff:', error);
            this.available = false;
            this.stopPullDiff();
        }
    }

    /**
     * Stop hash sender and set replica as unavailable
     */
    stopPullDiff() {
        if ( this.hashSenderInterval ) {
            clearInterval(this.hashSenderInterval);
            this.hashSenderInterval = null;
        }
    }

    /**
     * Disconnect the socket
     */
    disconnect() {
        if ( this.socket ) {
            this.socket.disconnect();
            this.isConnected = false;
        }

        this.stopPullDiff();
    }
}

// Create singleton instance
const replica = new ReplicaManager();

export default replica;
