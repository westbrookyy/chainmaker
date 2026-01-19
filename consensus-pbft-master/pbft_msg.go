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
	if msg == nil {
		return
	}

	var validators []string
	if to != "" {
		validators = append(validators, to)
	} else {
		// Safely access validatorSet.Validators with lock protection
		consensus.RLock()
		validators = make([]string, len(consensus.validatorSet.Validators))
		copy(validators, consensus.validatorSet.Validators)
		consensus.RUnlock()
	}

	if len(validators) == 0 {
		consensus.logger.Warnf("%s no validators to send consensus message", consensus.Id)
		return
	}

	consensus.logger.Infof("%s ready send consensus message to %v ", consensus.Id, validators)
	for _, v := range validators {
		// The recipient is yourself
		if v == consensus.Id {
			continue
		}
		go func(validator string) {
			// Marshal the message
			msgBz := mustMarshal(msg)
			if msgBz == nil {
				consensus.logger.Errorf("%s failed to marshal consensus message", consensus.Id)
				return
			}

			pbftMsg := &pbftpb.PBFTMsg{
				Type: getPBFTMsgType(msg),
				Msg:  msgBz,
			}

			// Marshal the PBFT message
			pbftMsgBz := mustMarshal(pbftMsg)
			if pbftMsgBz == nil {
				consensus.logger.Errorf("%s failed to marshal PBFT message", consensus.Id)
				return
			}

			netMsg := &netpb.NetMsg{
				Payload: pbftMsgBz,
				Type:    netpb.NetMsg_CONSENSUS_MSG,
				To:      validator,
			}
			consensus.logger.Infof("%s send consensus message to %s succeeded", consensus.Id, validator)
			consensus.msgbus.Publish(msgbus.SendConsensusMsg, netMsg)
		}(v)
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
		return
	}
	consensus.logger.Infof("%s send consensus pre-prepare", consensus.Id)
	consensus.sendConsensusMsg(prePrepare, to)
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
