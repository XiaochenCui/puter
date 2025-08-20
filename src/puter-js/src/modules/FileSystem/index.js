import io from '../../lib/socket.io/socket.io.esm.min.js';

// Operations
import copy from "./operations/copy.js";
import merkle from './operations/merkle.js';
import mkdir from "./operations/mkdir.js";
import move from "./operations/move.js";
import read from "./operations/read.js";
import rename from "./operations/rename.js";
import sign from "./operations/sign.js";
import space from "./operations/space.js";
import symlink from './operations/symlink.js';
import upload from "./operations/upload.js";
import write from "./operations/write.js";
// Why is this called deleteFSEntry instead of just delete? because delete is 
// a reserved keyword in javascript
import { AdvancedBase } from '../../../../putility/index.js';
import { ClientFS } from '../../lib/filesystem/ClientFS.js';
import FSItem from '../FSItem.js';
import deleteFSEntry from "./operations/deleteFSEntry.js";

export class PuterJSFileSystemModule extends AdvancedBase {

    space = space;
    mkdir = mkdir;
    copy = copy;
    rename = rename;
    upload = upload;
    read = read;
    // Why is this called deleteFSEntry instead of just delete? because delete is
    // a reserved keyword in javascript.
    delete = deleteFSEntry;
    move = move;
    write = write;
    sign = sign;
    symlink = symlink;
    merkle = merkle;
    
    FSItem = FSItem

    // Simple client-side filesystem
    clientFS = null;

    static NARI_METHODS = {
        stat: {
            positional: ['path'],
            firstarg_options: true,
            async fn (parameters) {
                // Simple switch: use client FS if local_replica_available is true
                if (window.local_replica_available === true && this.clientFS) {
                    return this.clientFS.stat(parameters);
                }
                // Otherwise use server filesystem
                const svc_fs = await this.context.services.aget('filesystem');
                return svc_fs.filesystem.stat(parameters);
            }
        },
        readdir: {
            positional: ['path'],
            firstarg_options: true,
            async fn (parameters) {
                // Simple switch: use client FS if local_replica_available is true
                if (window.local_replica_available === true && this.clientFS) {
                    return this.clientFS.readdir(parameters);
                }
                // Otherwise use server filesystem
                const svc_fs = await this.context.services.aget('filesystem');
                return svc_fs.filesystem.readdir(parameters);
            }
        },
    }

    /**
     * Creates a new instance with the given authentication token, API origin, and app ID,
     * and connects to the socket.
     *
     * @class
     * @param {Object} context - Context object containing authToken, APIOrigin, and appID.
     */
    constructor (context) {
        super();
        this.authToken = context.authToken;
        this.APIOrigin = context.APIOrigin;
        this.appID = context.appID;
        this.context = context;
        
        // Initialize simple client filesystem
        this.clientFS = new ClientFS();
        
        // Connect socket.
        this.initializeSocket();

        // We need to use `Object.defineProperty` instead of passing
        // `authToken` and `APIOrigin` because they will change.
        const api_info = {};
        Object.defineProperty(api_info, 'authToken', {
            get: () => this.authToken,
        });
        Object.defineProperty(api_info, 'APIOrigin', {
            get: () => this.APIOrigin,
        });
    }

    /**
     * Initializes the socket connection to the server using the current API origin.
     * If a socket connection already exists, it disconnects it before creating a new one.
     * Sets up various event listeners on the socket to handle different socket events like
     * connect, disconnect, reconnect, reconnect_attempt, reconnect_error, reconnect_failed, and error.
     *
     * @memberof FileSystem
     * @returns {void}
     */
    initializeSocket() {
        if (this.socket) {
            this.socket.disconnect();
        }

        this.socket = io(this.APIOrigin, {
            auth: {
                auth_token: this.authToken,
            }
        });

        this.bindSocketEvents();
    }

    bindSocketEvents() {
        this.socket.on('connect', () => {
            if(puter.debugMode)
                console.log('FileSystem Socket: Connected', this.socket.id);
        });

        this.socket.on('disconnect', () => {
            if(puter.debugMode)
                console.log('FileSystem Socket: Disconnected');
        });

        this.socket.on('reconnect', (attempt) => {
            if(puter.debugMode)
                console.log('FileSystem Socket: Reconnected', this.socket.id);
        });

        this.socket.on('reconnect_attempt', (attempt) => {
            if(puter.debugMode)
                console.log('FileSystem Socket: Reconnection Attemps', attempt);
        });

        this.socket.on('reconnect_error', (error) => {
            if(puter.debugMode)
                console.log('FileSystem Socket: Reconnection Error', error);
        });

        this.socket.on('reconnect_failed', () => {
            if(puter.debugMode)
                console.log('FileSystem Socket: Reconnection Failed');
        });

        this.socket.on('error', (error) => {
            if(puter.debugMode)
                console.error('FileSystem Socket Error:', error);
        });
    }

    /**
     * Sets a new authentication token and resets the socket connection with the updated token.
     *
     * @param {string} authToken - The new authentication token.
     * @memberof [FileSystem]
     * @returns {void}
     */
    setAuthToken (authToken) {
        this.authToken = authToken;
        // reset socket
        this.initializeSocket();
    }

    /**
     * Sets the API origin and resets the socket connection with the updated API origin.
     * 
     * @param {string} APIOrigin - The new API origin.
     * @memberof [Apps]
     * @returns {void}
     */
    setAPIOrigin (APIOrigin) {
        this.APIOrigin = APIOrigin;
        // reset socket
        this.initializeSocket();
    }
}
