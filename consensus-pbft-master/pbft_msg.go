/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.

SPDX-License-Identifier: Apache-2.0
*/

package pbft

import (
	"chainmaker.org/chainmaker/common/v2/msgbus"
	pbftpb "chainmaker.org/chainmaker/pb-go/v2/consensus/pbft"
	netpb "chainmaker.org/chainmaker/pb-go/v2/net"
	"github.com/gogo/protobuf/proto"
)

// ConsensusMsg implements transformation of structure and pb
type ConsensusMsg struct {
	Type pbftpb.PBFTMsgType
	Msg  interface{}
}

// sendConsensusMsg sends consensus msg, If to is an empty string, send to all validators
func (consensus *ConsensusPBFTImpl) sendConsensusMsg(msg proto.Message, to string) {
	consensus.logger.Infof("[%s] sendConsensusMsg: called with to='%s'", consensus.Id, to)
	if msg == nil {
		consensus.logger.Warnf("[%s] sendConsensusMsg: message is nil", consensus.Id)
		return
	}

	// Marshal the message once
	consensus.logger.Infof("[%s] sendConsensusMsg: starting to marshal consensus message", consensus.Id)
	msgBz := mustMarshal(msg)
	if msgBz == nil {
		consensus.logger.Errorf("[%s] sendConsensusMsg: failed to marshal consensus message", consensus.Id)
		return
	}
	consensus.logger.Infof("[%s] sendConsensusMsg: marshaled consensus message, size: %d", consensus.Id, len(msgBz))

	pbftMsg := &pbftpb.PBFTMsg{
		Type: getPBFTMsgType(msg),
		Msg:  msgBz,
	}

	// Marshal the PBFT message once
	consensus.logger.Infof("[%s] sendConsensusMsg: starting to marshal PBFT message", consensus.Id)
	pbftMsgBz := mustMarshal(pbftMsg)
	if pbftMsgBz == nil {
		consensus.logger.Errorf("[%s] sendConsensusMsg: failed to marshal PBFT message", consensus.Id)
		return
	}
	consensus.logger.Infof("[%s] sendConsensusMsg: marshaled PBFT message, size: %d, type: %v", 
		consensus.Id, len(pbftMsgBz), pbftMsg.Type)

	if to != "" {
		// Send to specific node (unicast)
		consensus.logger.Infof("[%s] sendConsensusMsg: sending to specific node %s", consensus.Id, to)
		netMsg := &netpb.NetMsg{
			Payload: pbftMsgBz,
			Type:    netpb.NetMsg_CONSENSUS_MSG,
			To:      to,
		}
		consensus.logger.Infof("[%s] publishing consensus message to msgbus (to: %s, type: %v, size: %d)", 
			consensus.Id, to, pbftMsg.Type, len(pbftMsgBz))
		consensus.msgbus.Publish(msgbus.SendConsensusMsg, netMsg)
		consensus.logger.Debugf("[%s] consensus message published to msgbus for %s", consensus.Id, to)
	} else {
		// Broadcast to all validators using consensus broadcast mechanism
		// Check validatorSet to ensure it's initialized
		consensus.logger.Infof("[%s] sendConsensusMsg: acquiring read lock to access validatorSet", consensus.Id)
		consensus.RLock()
		consensus.logger.Infof("[%s] sendConsensusMsg: read lock acquired successfully", consensus.Id)
		if consensus.validatorSet == nil {
			consensus.RUnlock()
			consensus.logger.Warnf("[%s] sendConsensusMsg: validatorSet is nil", consensus.Id)
			return
		}
		validatorCount := len(consensus.validatorSet.Validators)
		consensus.logger.Infof("[%s] sendConsensusMsg: validatorSet found, has %d validators: %v", 
			consensus.Id, validatorCount, consensus.validatorSet.Validators)
		consensus.RUnlock()
		consensus.logger.Infof("[%s] sendConsensusMsg: read lock released", consensus.Id)

		if validatorCount == 0 {
			consensus.logger.Warnf("[%s] sendConsensusMsg: no validators to send consensus message", consensus.Id)
			return
		}

		// Send broadcast message with empty To field to trigger consensus broadcast
		consensus.logger.Infof("[%s] sendConsensusMsg: broadcasting to all validators using consensus broadcast (total: %d)", 
			consensus.Id, validatorCount)
		netMsg := &netpb.NetMsg{
			Payload: pbftMsgBz,
			Type:    netpb.NetMsg_CONSENSUS_MSG,
			To:      "", // Empty To field triggers broadcast
		}
		consensus.logger.Infof("[%s] publishing consensus broadcast message to msgbus (type: %v, size: %d)", 
			consensus.Id, pbftMsg.Type, len(pbftMsgBz))
		consensus.logger.Infof("[%s] sendConsensusMsg: about to call msgbus.Publish", consensus.Id)
		consensus.msgbus.Publish(msgbus.SendConsensusMsg, netMsg)
		consensus.logger.Infof("[%s] sendConsensusMsg: consensus broadcast message published to msgbus", consensus.Id)
	}
}

// getPBFTMsgType gets PBFT message type from message
// Returns MSG_PREPREPARE as default for unknown types (should not happen in normal operation)
func getPBFTMsgType(msg proto.Message) pbftpb.PBFTMsgType {
	if msg == nil {
		return pbftpb.PBFTMsgType_MSG_PREPREPARE
	}
	switch msg.(type) {
	case *pbftpb.PrePrepare:
		return pbftpb.PBFTMsgType_MSG_PREPREPARE
	case *pbftpb.Prepare:
		return pbftpb.PBFTMsgType_MSG_PREPARE
	case *pbftpb.Commit:
		return pbftpb.PBFTMsgType_MSG_COMMIT
	case *pbftpb.ViewChange:
		return pbftpb.PBFTMsgType_MSG_VIEWCHANGE
	case *pbftpb.NewView:
		return pbftpb.PBFTMsgType_MSG_NEWVIEW
	default:
		// Unknown message type, return MSG_PREPREPARE as default
		// This should not happen in normal operation
		return pbftpb.PBFTMsgType_MSG_PREPREPARE
	}
}

// sendConsensusPrePrepare sends pre-prepare message
func (consensus *ConsensusPBFTImpl) sendConsensusPrePrepare(prePrepare *pbftpb.PrePrepare, to string) {
	if prePrepare == nil {
		consensus.logger.Warnf("[%s] sendConsensusPrePrepare: prePrepare is nil", consensus.Id)
		return
	}
	consensus.logger.Infof("[%s] sendConsensusPrePrepare: calling sendConsensusMsg (to='%s', sequence=%d, view=%d)", 
		consensus.Id, to, prePrepare.Sequence, prePrepare.View)
	consensus.sendConsensusMsg(prePrepare, to)
	consensus.logger.Infof("[%s] sendConsensusPrePrepare: sendConsensusMsg returned", consensus.Id)
}

// sendConsensusPrepare sends prepare message
func (consensus *ConsensusPBFTImpl) sendConsensusPrepare(prepare *pbftpb.Prepare, to string) {
	if prepare == nil {
		return
	}
	consensus.logger.Infof("%s send consensus prepare", consensus.Id)
	consensus.sendConsensusMsg(prepare, to)
}

// sendConsensusCommit sends commit message
func (consensus *ConsensusPBFTImpl) sendConsensusCommit(commit *pbftpb.Commit, to string) {
	if commit == nil {
		return
	}
	consensus.logger.Infof("%s send consensus commit", consensus.Id)
	consensus.sendConsensusMsg(commit, to)
}

// sendConsensusViewChange sends view change message
func (consensus *ConsensusPBFTImpl) sendConsensusViewChange(viewChange *pbftpb.ViewChange, to string) {
	if viewChange == nil {
		return
	}
	consensus.logger.Infof("%s send consensus view-change", consensus.Id)
	consensus.sendConsensusMsg(viewChange, to)
}

// sendConsensusNewView sends new view message
func (consensus *ConsensusPBFTImpl) sendConsensusNewView(newView *pbftpb.NewView, to string) {
	if newView == nil {
		return
	}
	consensus.logger.Infof("%s send consensus new-view", consensus.Id)
	consensus.sendConsensusMsg(newView, to)
}
