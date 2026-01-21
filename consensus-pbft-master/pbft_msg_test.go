/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.

SPDX-License-Identifier: Apache-2.0
*/

package pbft

import (
	"testing"

	"chainmaker.org/chainmaker/common/v2/msgbus"
	msgbusMock "chainmaker.org/chainmaker/common/v2/msgbus/mock"
	pbftpb "chainmaker.org/chainmaker/pb-go/v2/consensus/pbft"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

// TestGetPBFTMsgType 测试消息类型识别
func TestGetPBFTMsgType(t *testing.T) {
	tests := []struct {
		name string
		msg  proto.Message
		want pbftpb.PBFTMsgType
	}{
		{
			name: "PrePrepare message",
			msg:  &pbftpb.PrePrepare{},
			want: pbftpb.PBFTMsgType_MSG_PREPREPARE,
		},
		{
			name: "Prepare message",
			msg:  &pbftpb.Prepare{},
			want: pbftpb.PBFTMsgType_MSG_PREPARE,
		},
		{
			name: "Commit message",
			msg:  &pbftpb.Commit{},
			want: pbftpb.PBFTMsgType_MSG_COMMIT,
		},
		{
			name: "ViewChange message",
			msg:  &pbftpb.ViewChange{},
			want: pbftpb.PBFTMsgType_MSG_VIEWCHANGE,
		},
		{
			name: "NewView message",
			msg:  &pbftpb.NewView{},
			want: pbftpb.PBFTMsgType_MSG_NEWVIEW,
		},
		{
			name: "nil message",
			msg:  nil,
			want: pbftpb.PBFTMsgType_MSG_PREPREPARE,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getPBFTMsgType(tt.msg)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestSendConsensusMsg_Nil 测试发送nil消息
func TestSendConsensusMsg_Nil(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	msgBus := msgbusMock.NewMockMessageBus(ctrl)
	validators := []string{"node1", "node2", "node3"}
	vs := newValidatorSet(newTestLogger(), validators)

	consensus := &ConsensusPBFTImpl{
		Id:           "node1",
		msgbus:       msgBus,
		validatorSet: vs,
		logger:       newTestLogger(),
	}

	// 发送nil消息应该不执行任何操作
	consensus.sendConsensusMsg(nil, "")
	// 不应该调用Publish
}

// TestSendConsensusMsg_ToSpecificNode 测试发送到特定节点
func TestSendConsensusMsg_ToSpecificNode(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	msgBus := msgbusMock.NewMockMessageBus(ctrl)
	validators := []string{"node1", "node2", "node3"}
	vs := newValidatorSet(newTestLogger(), validators)

	consensus := &ConsensusPBFTImpl{
		Id:           "node1",
		msgbus:       msgBus,
		validatorSet: vs,
		logger:       newTestLogger(),
	}

	prePrepare := &pbftpb.PrePrepare{
		Primary:  "node1",
		View:     1,
		Sequence: 1,
		Digest:   []byte("digest"),
	}

	// 发送到特定节点
	msgBus.EXPECT().Publish(msgbus.SendConsensusMsg, gomock.Any()).Times(1)

	consensus.sendConsensusMsg(prePrepare, "node2")
}

// TestSendConsensusMsg_ToAll 测试广播到所有节点
func TestSendConsensusMsg_ToAll(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	msgBus := msgbusMock.NewMockMessageBus(ctrl)
	validators := []string{"node1", "node2", "node3"}
	vs := newValidatorSet(newTestLogger(), validators)

	consensus := &ConsensusPBFTImpl{
		Id:           "node1",
		msgbus:       msgBus,
		validatorSet: vs,
		logger:       newTestLogger(),
	}

	prePrepare := &pbftpb.PrePrepare{
		Primary:  "node1",
		View:     1,
		Sequence: 1,
		Digest:   []byte("digest"),
	}

	// 广播到所有节点（除了自己），应该发送到node2和node3
	msgBus.EXPECT().Publish(msgbus.SendConsensusMsg, gomock.Any()).Times(2)

	consensus.sendConsensusMsg(prePrepare, "")
}

// TestSendConsensusMsg_SkipSelf 测试跳过自己
func TestSendConsensusMsg_SkipSelf(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	msgBus := msgbusMock.NewMockMessageBus(ctrl)
	validators := []string{"node1", "node2"}
	vs := newValidatorSet(newTestLogger(), validators)

	consensus := &ConsensusPBFTImpl{
		Id:           "node1",
		msgbus:       msgBus,
		validatorSet: vs,
		logger:       newTestLogger(),
	}

	prePrepare := &pbftpb.PrePrepare{
		Primary:  "node1",
		View:     1,
		Sequence: 1,
		Digest:   []byte("digest"),
	}

	// 应该只发送到node2，不发送给自己
	msgBus.EXPECT().Publish(msgbus.SendConsensusMsg, gomock.Any()).Times(1)

	consensus.sendConsensusMsg(prePrepare, "")
}

// TestSendConsensusPrePrepare 测试发送PrePrepare消息
func TestSendConsensusPrePrepare(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	msgBus := msgbusMock.NewMockMessageBus(ctrl)
	validators := []string{"node1", "node2"}
	vs := newValidatorSet(newTestLogger(), validators)

	consensus := &ConsensusPBFTImpl{
		Id:           "node1",
		msgbus:       msgBus,
		validatorSet: vs,
		logger:       newTestLogger(),
	}

	prePrepare := &pbftpb.PrePrepare{
		Primary:  "node1",
		View:     1,
		Sequence: 1,
		Digest:   []byte("digest"),
	}

	msgBus.EXPECT().Publish(msgbus.SendConsensusMsg, gomock.Any()).Times(1)

	consensus.sendConsensusPrePrepare(prePrepare, "node2")
}

// TestSendConsensusPrePrepare_Nil 测试发送nil PrePrepare消息
func TestSendConsensusPrePrepare_Nil(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	msgBus := msgbusMock.NewMockMessageBus(ctrl)
	validators := []string{"node1", "node2"}
	vs := newValidatorSet(newTestLogger(), validators)

	consensus := &ConsensusPBFTImpl{
		Id:           "node1",
		msgbus:       msgBus,
		validatorSet: vs,
		logger:       newTestLogger(),
	}

	// 发送nil消息应该不执行任何操作
	consensus.sendConsensusPrePrepare(nil, "node2")
}

// TestSendConsensusPrepare 测试发送Prepare消息
func TestSendConsensusPrepare(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	msgBus := msgbusMock.NewMockMessageBus(ctrl)
	validators := []string{"node1", "node2"}
	vs := newValidatorSet(newTestLogger(), validators)

	consensus := &ConsensusPBFTImpl{
		Id:           "node1",
		msgbus:       msgBus,
		validatorSet: vs,
		logger:       newTestLogger(),
	}

	prepare := &pbftpb.Prepare{
		NodeId:   "node1",
		View:     1,
		Sequence: 1,
		Digest:   []byte("digest"),
	}

	msgBus.EXPECT().Publish(msgbus.SendConsensusMsg, gomock.Any()).Times(1)

	consensus.sendConsensusPrepare(prepare, "node2")
}

// TestSendConsensusCommit 测试发送Commit消息
func TestSendConsensusCommit(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	msgBus := msgbusMock.NewMockMessageBus(ctrl)
	validators := []string{"node1", "node2"}
	vs := newValidatorSet(newTestLogger(), validators)

	consensus := &ConsensusPBFTImpl{
		Id:           "node1",
		msgbus:       msgBus,
		validatorSet: vs,
		logger:       newTestLogger(),
	}

	commit := &pbftpb.Commit{
		NodeId:   "node1",
		View:     1,
		Sequence: 1,
		Digest:   []byte("digest"),
	}

	msgBus.EXPECT().Publish(msgbus.SendConsensusMsg, gomock.Any()).Times(1)

	consensus.sendConsensusCommit(commit, "node2")
}

// TestSendConsensusViewChange 测试发送ViewChange消息
func TestSendConsensusViewChange(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	msgBus := msgbusMock.NewMockMessageBus(ctrl)
	validators := []string{"node1", "node2"}
	vs := newValidatorSet(newTestLogger(), validators)

	consensus := &ConsensusPBFTImpl{
		Id:           "node1",
		msgbus:       msgBus,
		validatorSet: vs,
		logger:       newTestLogger(),
	}

	viewChange := &pbftpb.ViewChange{
		NodeId:   "node1",
		CurView:  0,
		NextView: 1,
		Sequence: 1,
	}

	msgBus.EXPECT().Publish(msgbus.SendConsensusMsg, gomock.Any()).Times(1)

	consensus.sendConsensusViewChange(viewChange, "node2")
}

// TestSendConsensusNewView 测试发送NewView消息
func TestSendConsensusNewView(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	msgBus := msgbusMock.NewMockMessageBus(ctrl)
	validators := []string{"node1", "node2"}
	vs := newValidatorSet(newTestLogger(), validators)

	consensus := &ConsensusPBFTImpl{
		Id:           "node1",
		msgbus:       msgBus,
		validatorSet: vs,
		logger:       newTestLogger(),
	}

	newView := &pbftpb.NewView{
		NodeId:      "node1",
		CurView:     0,
		NextView:    1,
		NewSequence: 1,
	}

	msgBus.EXPECT().Publish(msgbus.SendConsensusMsg, gomock.Any()).Times(1)

	consensus.sendConsensusNewView(newView, "node2")
}

// TestSendConsensusMsg_EmptyValidators 测试空validators列表的情况
func TestSendConsensusMsg_EmptyValidators(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	msgBus := msgbusMock.NewMockMessageBus(ctrl)
	// 创建空的validators列表
	vs := newValidatorSet(newTestLogger(), []string{})

	consensus := &ConsensusPBFTImpl{
		Id:           "node1",
		msgbus:       msgBus,
		validatorSet: vs,
		logger:       newTestLogger(),
	}

	prePrepare := &pbftpb.PrePrepare{
		Primary:  "node1",
		View:     1,
		Sequence: 1,
		Digest:   []byte("digest"),
	}

	// 空validators列表时不应该发送任何消息
	// 不应该调用Publish
	consensus.sendConsensusMsg(prePrepare, "")
}
