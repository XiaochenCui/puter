"use strict";

const grpc = require('@grpc/grpc-js');
const path = require('path');

// gRPC generated code
const genDir = path.join(__dirname, '../../../../../fs_tree_manager/js');
const {
    FSTreeManagerClient
} = require(path.join(genDir, 'fs_tree_manager_grpc_pb.js'));
const {
    FSEntry
} = require(path.join(genDir, 'fs_tree_manager_pb.js'));

// protobuf built-in types
const { Struct } = require("google-protobuf/google/protobuf/struct_pb.js");

// Create gRPC client
const client = new FSTreeManagerClient('localhost:50052', grpc.credentials.createInsecure());

/**
 * Sends a filesystem update event to the gRPC service
 * @param {Object} fsUpdateEvent - The filesystem update event data
 * @param {Object} fsUpdateEvent.metadata - The metadata for the FSEntry
 * @returns {Promise<void>} - Resolves when the update is sent successfully
 * @throws {Error} - If the gRPC call fails
 */
async function sendFsUpdate(fsUpdateEvent) {
    return new Promise((resolve, reject) => {
        if (!fsUpdateEvent.metadata) {
            reject(new Error('Metadata is required'));
            return;
        }

        // Build the FSEntry message
        const fsEntry = buildFsEntry(fsUpdateEvent.metadata);

        // Call the NewDirectory RPC
        client.newDirectory(fsEntry, (err, response) => {
            if (err) {
                reject(new Error(`Failed to send fs update: ${err.message}`));
                return;
            }
            resolve();
        });
    });
}

/**
 * Recursively sanitize values so they can be accepted by google.protobuf.Struct.
 * - undefined -> null
 * - Date -> ISO string
 * - BigInt -> string
 * - Buffer/Uint8Array -> base64 string
 * - Map -> plain object
 * - Set -> array
 * - Other non-JSON types -> string fallback
 */
function sanitizeForStruct(value) {
    if (value === undefined) return null;
    if (value === null) return null;

    const t = typeof value;
    if (t === "string" || t === "number" || t === "boolean") return value;

    if (Array.isArray(value)) {
        return value.map(sanitizeForStruct);
    }

    if (value instanceof Date) return value.toISOString();

    if (typeof Buffer !== "undefined" && Buffer.isBuffer(value)) {
        return value.toString("base64");
    }
    if (value instanceof Uint8Array) {
        return Buffer.from(value).toString("base64");
    }

    if (value instanceof Map) {
        return Object.fromEntries(
            Array.from(value.entries()).map(([k, v]) => [k, sanitizeForStruct(v)])
        );
    }
    if (value instanceof Set) {
        return Array.from(value).map(sanitizeForStruct);
    }

    if (value && value.constructor === Object) {
        const out = {};
        for (const [k, v] of Object.entries(value)) {
            out[k] = sanitizeForStruct(v);
        }
        return out;
    }

    if (t === "bigint") return value.toString();

    if (typeof value.toJSON === "function") {
        return sanitizeForStruct(value.toJSON());
    }

    return String(value);
}

/**
 * Build an FSEntry message from a plain JS metadata object.
 * @param {Object} metadataObj - The raw metadata object.
 * @returns {FSEntry}
 */
function buildFsEntry(metadataObj) {
    const sanitized = sanitizeForStruct(metadataObj);
    const struct = Struct.fromJavaScript(sanitized);

    const fsEntry = new FSEntry();
    fsEntry.setMetadata(struct);
    return fsEntry;
}

module.exports = {
    sendFsUpdate
};
