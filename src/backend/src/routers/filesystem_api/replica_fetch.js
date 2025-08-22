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
const eggspress = require('../../api/eggspress.js');

// -----------------------------------------------------------------------//
// POST /api/fs/replica/fetch
// -----------------------------------------------------------------------//
module.exports = eggspress('/api/fs/replica/fetch', {
  subdomain: 'api',
  auth2: true,
  verified: true,
  fs: true,
  json: true,
  allowedMethods: ['POST'],
}, async (req, res, next) => {
  let log;
  {
    const x = Context.get();
    log = x.get('services').get('log-service').create('replica-fetch', {
      concern: 'filesystem',
    });
    log.info(`replica/fetch: ${JSON.stringify(req.body)}`);
  }

  // Print the request content as requested
  console.log('Replica fetch request:', {
    path: req.body.path,
    user: req.user?.username || 'unknown',
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
    FetchReplicaRequest
  } = require(path.join(genDir, 'fs_tree_manager_pb.js'));

  const client = new FSTreeManagerClient('localhost:50052', grpc.credentials.createInsecure());

  // Recursively convert MerkleTree (protobuf -> plain JS)
  function convertMerkleTree(node) {
    if (!node) return null;
    const metadataStruct = node.getMetadata && node.getMetadata();
    return {
      name: node.getName?.() ?? undefined,
      merkle_hash: node.getMerkleHash?.() ?? undefined,
      metadata: metadataStruct && metadataStruct.toJavaScript
        ? metadataStruct.toJavaScript()
        : {},
      children: (node.getChildrenList ? node.getChildrenList() : [])
        .map(convertMerkleTree),
    };
  }

  // Build the request message
  const requestMsg = new FetchReplicaRequest();
  requestMsg.setUserName(req.user.username);

  client.fetchReplica(requestMsg, (err, resp) => {
    if (err) {
      console.error('FetchReplica error:', err);
      return res.status(500).json({
        success: false,
        error: { message: 'Failed to fetch replica' }
      });
    }

    // resp.getTree() returns a MerkleTree message
    const treeJs = convertMerkleTree(resp.getTree());

    res.json({
      success: true,
      data: treeJs,
      name: req.body.name
    });
  });
});
