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
const { Context } = require('../../../util/context.js');

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

    // ----------------------------
    // gRPC generated code
    // ----------------------------
    const grpc = require('@grpc/grpc-js');
    const path = require('path');

    // Adjust these paths to where your generated files live:
    const genDir = path.join(__dirname, '../../../../../fs_tree_manager/js');
    const {
      FSTreeManagerClient
    } = require(path.join(genDir, 'fs_tree_manager_grpc_pb.js'));
    const {
      FetchReplicaRequest
    } = require(path.join(genDir, 'fs_tree_manager_pb.js'));

    const client = new FSTreeManagerClient('localhost:50052', grpc.credentials.createInsecure());

    // Build the request message
    const requestMsg = new FetchReplicaRequest();
    requestMsg.setUserName(socket.user.username);

    client.fetchReplica(requestMsg, (err, resp) => {
      if (err) {
        log.error('FetchReplica error:', err);
        return socket.emit('replica/fetch/error', {
          success: false,
          error: { message: 'Failed to fetch replica', details: err.message }
        });
      }

      // Convert protobuf response to plain JavaScript
      const tree = resp.getTree();
      
      // Get the nodes map and root ID
      const nodesMap = tree.getNodesMap();
      const rootId = tree.getRootId();
      
      // Convert nodes map to plain JavaScript object
      const nodes = {};
      nodesMap.forEach((node, nodeId) => {
        nodes[nodeId] = {
          id: node.getId(),
          merkle_hash: node.getMerkleHash(),
          children_ids: node.getChildrenIdsList(),
          parent_id: node.getParentId(),
          fs_entry: node.getFsEntry() ? node.getFsEntry().getMetadata().toJavaScript() : {}
        };
      });

      socket.emit('replica/fetch/success', {
        success: true,
        data: {
          root_id: rootId,
          nodes: nodes
        }
      });
    });
  }
};
