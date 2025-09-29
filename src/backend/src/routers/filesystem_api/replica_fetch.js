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

"use strict";
const { Context } = require('../../util/context.js');

// -----------------------------------------------------------------------//
// WebSocket handler for replica/fetch
// -----------------------------------------------------------------------//
module.exports = {
  event: 'replica/fetch',
  handler: async (socket, data) => {
    let log;
    {
      const x = Context.get();
      log = x.get('services').get('log-service').create('replica-fetch', {
        concern: 'filesystem',
      });
      log.info(`replica/fetch: ${JSON.stringify(data)}`);
    }

    // Print the request content as requested
    console.log('Replica fetch request:', {
      path: data.path,
      user: socket.user?.username || 'unknown',
      timestamp: new Date().toISOString()
    });

    // ----------------------------
    // gRPC generated code
    // ----------------------------
    const grpc = require('@grpc/grpc-js');
    const path = require('path');

    // Adjust these paths to where your generated files live:
    const genDir = path.join(__dirname, '../../../../fs_tree_manager/js');
    const {
      FSTreeManagerClient
    } = require(path.join(genDir, 'fs_tree_manager_grpc_pb.js'));
    const {
      FetchReplicaRequest,
      FSEntry
    } = require(path.join(genDir, 'fs_tree_manager_pb.js'));
    const {
      structpb
    } = require('google-protobuf');

    const client = new FSTreeManagerClient('localhost:50052', grpc.credentials.createInsecure());

    // Recursively convert MerkleTree (protobuf -> plain JS)
    function convertMerkleTree(node) {
      if (!node) return null;
      
      const fsEntry = node.getFsEntry && node.getFsEntry();
      const metadata = fsEntry && fsEntry.getMetadata ? fsEntry.getMetadata().toJavaScript() : {};
      
      return {
        name: node.getName?.() ?? undefined,
        merkle_hash: node.getMerkleHash?.() ?? undefined,
        metadata: metadata,
        children: (node.getChildrenList ? node.getChildrenList() : [])
          .map(convertMerkleTree),
      };
    }

    // Build the request message
    const requestMsg = new FetchReplicaRequest();
    requestMsg.setUserName(socket.user.username);
    
    // Create FSEntry with metadata from the request data
    const fsEntryMsg = new FSEntry();
    const metadataStruct = new structpb.Struct();
    const metadata = data.metadata || {};
    metadataStruct.setFields(metadata);
    fsEntryMsg.setMetadata(metadataStruct);
    fsEntryMsg.setName(data.name || '');
    fsEntryMsg.setPath(data.path || '');
    
    requestMsg.setFsEntry(fsEntryMsg);

    client.fetchReplica(requestMsg, (err, resp) => {
      if (err) {
        log.error('FetchReplica error:', err);
        return socket.emit('replica/fetch/error', {
          success: false,
          error: { message: 'Failed to fetch replica', details: err.message }
        });
      }

      try {
        // resp.getTree() returns a MerkleTree message
        const treeJs = convertMerkleTree(resp.getTree());

        socket.emit('replica/fetch/success', {
          success: true,
          data: treeJs,
          name: data.name
        });
      } catch (conversionError) {
        log.error('Error converting MerkleTree:', conversionError);
        socket.emit('replica/fetch/error', {
          success: false,
          error: { message: 'Failed to process replica data', details: conversionError.message }
        });
      }
    });
  }
};
