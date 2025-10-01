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
        if (this.isInitialized) {
            return;
        }

        this.authToken = context.authToken;
        this.APIOrigin = context.APIOrigin;
        this.appID = context.appID;

        // Fetch username from whoami endpoint if not provided in context
        if (!context.username) {
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
                    Authorization: `Bearer ${this.authToken}`
                }
            });

            const result = await resp.json();
            return result.username;
        } catch (error) {
            console.error('Replica Manager: Failed to fetch username from whoami endpoint:', error);
            throw error;
        }
    }

    /**
     * Connect to the websocket
     */
    connect() {
        if (this.socket) {
            this.socket.disconnect();
        }

        this.socket = io(this.APIOrigin, {
            auth: {
                auth_token: this.authToken,
            }
        });

        this.bindEvents();
    }

    /**
     * Bind websocket events
     */
    bindEvents() {
        this.socket.on('connect', () => {
            this.isConnected = true;
            if (puter.debugMode) {
                console.log('Replica Manager: Connected', this.socket.id);
            }

            // Automatically fetch user's root path on connection
            this.fetchUserRoot();
            
            // Start background task to send FSTree hash
            this.startHashSender();
        });

        this.socket.on('disconnect', () => {
            this.isConnected = false;
            if (puter.debugMode) {
                console.log('Replica Manager: Disconnected');
            }
        });

        this.socket.on('reconnect', (attempt) => {
            this.isConnected = true;
            if (puter.debugMode) {
                console.log('Replica Manager: Reconnected', this.socket.id);
            }

            // Refetch user's root path on reconnection
            this.fetchUserRoot();
            
            // Restart hash sender on reconnection
            this.startHashSender();
        });

        this.socket.on('reconnect_attempt', (attempt) => {
            if (puter.debugMode) {
                console.log('Replica Manager: Reconnection Attempt', attempt);
            }
        });

        this.socket.on('reconnect_error', (error) => {
            if (puter.debugMode) {
                console.log('Replica Manager: Reconnection Error', error);
            }
        });

        this.socket.on('reconnect_failed', () => {
            if (puter.debugMode) {
                console.log('Replica Manager: Reconnection Failed');
            }
        });

        this.socket.on('error', (error) => {
            if (puter.debugMode) {
                console.error('Replica Manager Error:', error);
            }
        });

        this.socket.on('replica/fetch/success', (data) => {
            this.handleReplicaSuccess(data);
        });

        this.socket.on('replica/fetch/error', (data) => {
            this.handleReplicaError(data);
        });
    }

    /**
     * Fetch the user's root path
     */
    fetchUserRoot() {
        if (!this.username) {
            console.warn('Replica Manager: No username available for fetching root');
            return;
        }

        const userRootPath = `/${this.username}`;

        this.socket.emit('replica/fetch', {
            path: userRootPath,
            requestId: 'user_root' // Special request ID for user root
        });
    }

    /**
     * Handle successful replica fetch
     */
    handleReplicaSuccess(data) {
        console.log('Replica Manager: Received replica data:', data);

        // Initialize the FSTree
        window.FSTree = new FSTree(data.data);
        this.available = true;

        // Emit custom event for other parts of the app
        if (typeof window !== 'undefined') {
            window.dispatchEvent(new CustomEvent('replica:ready', {
                detail: { data: data.data }
            }));
        }
    }

    /**
     * Handle replica fetch error
     */
    handleReplicaError(data) {
        console.error('Replica Manager: Failed to fetch replica:', data);

        // Emit custom event for error handling
        if (typeof window !== 'undefined') {
            window.dispatchEvent(new CustomEvent('replica:error', {
                detail: { error: data }
            }));
        }
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

    /**
     * Start background task to send FSTree hash to server
     */
    startHashSender() {
        // Clear any existing interval
        if (this.hashSenderInterval) {
            clearInterval(this.hashSenderInterval);
        }

        // Send hash immediately if FSTree is available
        this.sendFSTreeHash();

        // Set up interval to send hash every 5 seconds
        this.hashSenderInterval = setInterval(() => {
            this.sendFSTreeHash();
        }, 5000);
    }

    /**
     * Send FSTree root hash to server
     */
    sendFSTreeHash() {
        if (!this.isSocketConnected() || !window.FSTree) {
            return;
        }

        try {
            const rootNode = window.FSTree.nodes[window.FSTree.rootId];
            if (rootNode && rootNode.merkle_hash) {
                this.socket.emit('fstree/hash', {
                    hash: rootNode.merkle_hash,
                    timestamp: Date.now()
                });
                
                if (puter.debugMode) {
                    console.log('Replica Manager: Sent FSTree hash:', rootNode.merkle_hash);
                }
            }
        } catch (error) {
            if (puter.debugMode) {
                console.error('Replica Manager: Failed to send FSTree hash:', error);
            }
        }
    }

    /**
     * Disconnect the socket
     */
    disconnect() {
        if (this.socket) {
            this.socket.disconnect();
            this.isConnected = false;
        }
        
        // Clear hash sender interval
        if (this.hashSenderInterval) {
            clearInterval(this.hashSenderInterval);
            this.hashSenderInterval = null;
        }
    }
}

// Create singleton instance
const replica = new ReplicaManager();

export default replica;
