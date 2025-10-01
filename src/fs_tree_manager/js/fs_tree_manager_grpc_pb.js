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

function serialize_fs_tree_manager_FetchReplicaRequest(arg) {
  if (!(arg instanceof fs_tree_manager_pb.FetchReplicaRequest)) {
    throw new Error('Expected argument of type fs_tree_manager.FetchReplicaRequest');
  }
  return Buffer.from(arg.serializeBinary());
}

function deserialize_fs_tree_manager_FetchReplicaRequest(buffer_arg) {
  return fs_tree_manager_pb.FetchReplicaRequest.deserializeBinary(new Uint8Array(buffer_arg));
}

function serialize_fs_tree_manager_FetchReplicaResponse(arg) {
  if (!(arg instanceof fs_tree_manager_pb.FetchReplicaResponse)) {
    throw new Error('Expected argument of type fs_tree_manager.FetchReplicaResponse');
  }
  return Buffer.from(arg.serializeBinary());
}

function deserialize_fs_tree_manager_FetchReplicaResponse(buffer_arg) {
  return fs_tree_manager_pb.FetchReplicaResponse.deserializeBinary(new Uint8Array(buffer_arg));
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
    requestType: fs_tree_manager_pb.FetchReplicaRequest,
    responseType: fs_tree_manager_pb.FetchReplicaResponse,
    requestSerialize: serialize_fs_tree_manager_FetchReplicaRequest,
    requestDeserialize: deserialize_fs_tree_manager_FetchReplicaRequest,
    responseSerialize: serialize_fs_tree_manager_FetchReplicaResponse,
    responseDeserialize: deserialize_fs_tree_manager_FetchReplicaResponse,
  },
  // It isn't named "mkdir" since it doesn't handle the various parameters
// supported by "mkdir."
newDirectory: {
    path: '/fs_tree_manager.FSTreeManager/NewDirectory',
    requestStream: false,
    responseStream: false,
    requestType: fs_tree_manager_pb.FSEntry,
    responseType: google_protobuf_empty_pb.Empty,
    requestSerialize: serialize_fs_tree_manager_FSEntry,
    requestDeserialize: deserialize_fs_tree_manager_FSEntry,
    responseSerialize: serialize_google_protobuf_Empty,
    responseDeserialize: deserialize_google_protobuf_Empty,
  },
};

exports.FSTreeManagerClient = grpc.makeGenericClientConstructor(FSTreeManagerService, 'FSTreeManager');
