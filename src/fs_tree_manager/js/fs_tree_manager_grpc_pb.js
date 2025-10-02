// GENERATED CODE -- DO NOT EDIT!

'use strict';
var grpc = require('@grpc/grpc-js');
var fs_tree_manager_pb = require('./fs_tree_manager_pb.js');
var google_protobuf_struct_pb = require('google-protobuf/google/protobuf/struct_pb.js');
var google_protobuf_empty_pb = require('google-protobuf/google/protobuf/empty_pb.js');

function serialize_fs_tree_manager_FSEntry(arg) {
  if (!(arg instanceof fs_tree_manager_pb.FSEntry)) {
    throw new Error('Expected argument of type fs_tree_manager.FSEntry');
  }
  return Buffer.from(arg.serializeBinary());
}

function deserialize_fs_tree_manager_FSEntry(buffer_arg) {
  return fs_tree_manager_pb.FSEntry.deserializeBinary(new Uint8Array(buffer_arg));
}

function serialize_fs_tree_manager_MerkleTree(arg) {
  if (!(arg instanceof fs_tree_manager_pb.MerkleTree)) {
    throw new Error('Expected argument of type fs_tree_manager.MerkleTree');
  }
  return Buffer.from(arg.serializeBinary());
}

function deserialize_fs_tree_manager_MerkleTree(buffer_arg) {
  return fs_tree_manager_pb.MerkleTree.deserializeBinary(new Uint8Array(buffer_arg));
}

function serialize_fs_tree_manager_UserName(arg) {
  if (!(arg instanceof fs_tree_manager_pb.UserName)) {
    throw new Error('Expected argument of type fs_tree_manager.UserName');
  }
  return Buffer.from(arg.serializeBinary());
}

function deserialize_fs_tree_manager_UserName(buffer_arg) {
  return fs_tree_manager_pb.UserName.deserializeBinary(new Uint8Array(buffer_arg));
}

function serialize_google_protobuf_Empty(arg) {
  if (!(arg instanceof google_protobuf_empty_pb.Empty)) {
    throw new Error('Expected argument of type google.protobuf.Empty');
  }
  return Buffer.from(arg.serializeBinary());
}

function deserialize_google_protobuf_Empty(buffer_arg) {
  return google_protobuf_empty_pb.Empty.deserializeBinary(new Uint8Array(buffer_arg));
}


var FSTreeManagerService = exports.FSTreeManagerService = {
  fetchReplica: {
    path: '/fs_tree_manager.FSTreeManager/FetchReplica',
    requestStream: false,
    responseStream: false,
    requestType: fs_tree_manager_pb.UserName,
    responseType: fs_tree_manager_pb.MerkleTree,
    requestSerialize: serialize_fs_tree_manager_UserName,
    requestDeserialize: deserialize_fs_tree_manager_UserName,
    responseSerialize: serialize_fs_tree_manager_MerkleTree,
    responseDeserialize: deserialize_fs_tree_manager_MerkleTree,
  },
  // We provide simple New/Remove APIs as a straightforward way to accommodate
// the wide variety of file system operations. For simplicity, these APIs do
// not automatically update parent or child FSEntries.
newFSEntry: {
    path: '/fs_tree_manager.FSTreeManager/NewFSEntry',
    requestStream: false,
    responseStream: false,
    requestType: fs_tree_manager_pb.FSEntry,
    responseType: google_protobuf_empty_pb.Empty,
    requestSerialize: serialize_fs_tree_manager_FSEntry,
    requestDeserialize: deserialize_fs_tree_manager_FSEntry,
    responseSerialize: serialize_google_protobuf_Empty,
    responseDeserialize: deserialize_google_protobuf_Empty,
  },
  removeFSEntry: {
    path: '/fs_tree_manager.FSTreeManager/RemoveFSEntry',
    requestStream: false,
    responseStream: false,
    requestType: fs_tree_manager_pb.FSEntry,
    responseType: google_protobuf_empty_pb.Empty,
    requestSerialize: serialize_fs_tree_manager_FSEntry,
    requestDeserialize: deserialize_fs_tree_manager_FSEntry,
    responseSerialize: serialize_google_protobuf_Empty,
    responseDeserialize: deserialize_google_protobuf_Empty,
  },
  // For any fs operations that cannot be handled by New/Remove APIs, just purge
// the replica.
purgeReplica: {
    path: '/fs_tree_manager.FSTreeManager/PurgeReplica',
    requestStream: false,
    responseStream: false,
    requestType: fs_tree_manager_pb.UserName,
    responseType: google_protobuf_empty_pb.Empty,
    requestSerialize: serialize_fs_tree_manager_UserName,
    requestDeserialize: deserialize_fs_tree_manager_UserName,
    responseSerialize: serialize_google_protobuf_Empty,
    responseDeserialize: deserialize_google_protobuf_Empty,
  },
};

exports.FSTreeManagerClient = grpc.makeGenericClientConstructor(FSTreeManagerService, 'FSTreeManager');
