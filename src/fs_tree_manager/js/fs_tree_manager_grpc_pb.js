// GENERATED CODE -- DO NOT EDIT!

'use strict';
var grpc = require('@grpc/grpc-js');
var fs_tree_manager_pb = require('./fs_tree_manager_pb.js');
var google_protobuf_struct_pb = require('google-protobuf/google/protobuf/struct_pb.js');

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
};

exports.FSTreeManagerClient = grpc.makeGenericClientConstructor(FSTreeManagerService, 'FSTreeManager');
