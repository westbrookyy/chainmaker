/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.

SPDX-License-Identifier: Apache-2.0
*/

package pbft

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"chainmaker.org/chainmaker/chainconf/v2"
	"chainmaker.org/chainmaker/common/v2/msgbus"
	consensusUtils "chainmaker.org/chainmaker/consensus-utils/v2"
	"chainmaker.org/chainmaker/consensus-utils/v2/wal_service"
	"chainmaker.org/chainmaker/lws"
	"chainmaker.org/chainmaker/pb-go/v2/common"
	"chainmaker.org/chainmaker/pb-go/v2/config"
	consensuspb "chainmaker.org/chainmaker/pb-go/v2/consensus"
	pbftpb "chainmaker.org/chainmaker/pb-go/v2/consensus/pbft"
	netpb "chainmaker.org/chainmaker/pb-go/v2/net"
	"chainmaker.org/chainmaker/protocol/v2"
	"chainmaker.org/chainmaker/utils/v2"

	"github.com/gogo/protobuf/proto"
)

var (
	nilHash = []byte("NilHash")
)

func mustMarshalRecover(msg proto.Message) (data []byte) {
	var err error
	defer func() {
		if recover() != nil {
			if err != nil {
				return
			}
		}
	}()

	data, err = proto.Marshal(msg)
	if err != nil {
		return nil
	}
	return
}

func mustMarshalWal(msg proto.Message) (data []byte) {
	var res []byte
	for {
		if res = mustMarshalRecover(msg); res != nil {
			break
		}
	}
	return res
}

// isPrimary checks if the current node is the primary node for the current view
func (consensus *ConsensusPBFTImpl) isPrimary() bool {
	primary, err := consensus.validatorSet.GetPrimary(consensus.View, consensus.Sequence)
	if err != nil {
		return false
	}
	return primary == consensus.Id
}

// signPrePrepare signs a PrePrepare message
func (consensus *ConsensusPBFTImpl) signPrePrepare(prePrepare *pbftpb.PrePrepare) error {
	// Create a copy without endorsement for signing
	prePrepareCopy := &pbftpb.PrePrepare{
		Primary:  prePrepare.Primary,
		View:     prePrepare.View,
		Sequence: prePrepare.Sequence,
		Digest:   prePrepare.Digest,
		Block:    prePrepare.Block,
		TxsRwSet: prePrepare.TxsRwSet,
	}
	prePrepareBz := mustMarshal(prePrepareCopy)

	sig, err := consensus.signer.Sign(consensus.chainConf.ChainConfig().Crypto.Hash, prePrepareBz)
	if err != nil {
		return err
	}

	serializeMember, err := consensus.signer.GetMember()
	if err != nil {
		return err
	}

	prePrepare.Endorsement = &common.EndorsementEntry{
		Signer:    serializeMember,
		Signature: sig,
	}
	return nil
}

// signPrepare signs a Prepare message
func (consensus *ConsensusPBFTImpl) signPrepare(prepare *pbftpb.Prepare) error {
	prepareCopy := &pbftpb.Prepare{
		NodeId:   prepare.NodeId,
		View:     prepare.View,
		Sequence: prepare.Sequence,
		Digest:   prepare.Digest,
	}
	prepareBz := mustMarshal(prepareCopy)

	sig, err := consensus.signer.Sign(consensus.chainConf.ChainConfig().Crypto.Hash, prepareBz)
	if err != nil {
		return err
	}

	serializeMember, err := consensus.signer.GetMember()
	if err != nil {
		return err
	}

	prepare.Endorsement = &common.EndorsementEntry{
		Signer:    serializeMember,
		Signature: sig,
	}
	return nil
}

// signCommit signs a Commit message
func (consensus *ConsensusPBFTImpl) signCommit(commit *pbftpb.Commit) error {
	commitCopy := &pbftpb.Commit{
		NodeId:   commit.NodeId,
		View:     commit.View,
		Sequence: commit.Sequence,
		Digest:   commit.Digest,
	}
	commitBz := mustMarshal(commitCopy)

	sig, err := consensus.signer.Sign(consensus.chainConf.ChainConfig().Crypto.Hash, commitBz)
	if err != nil {
		return err
	}

	serializeMember, err := consensus.signer.GetMember()
	if err != nil {
		return err
	}

	commit.Endorsement = &common.EndorsementEntry{
		Signer:    serializeMember,
		Signature: sig,
	}
	return nil
}

// procPrePrepare processes a PrePrepare message
func (consensus *ConsensusPBFTImpl) procPrePrepare(prePrepare *pbftpb.PrePrepare) {
	if prePrepare == nil {
		return
	}

	consensus.logger.Infof("[%s](%d/%d/%s) receive pre-prepare from %s (%d/%d/%x)",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step,
		prePrepare.Primary, prePrepare.Sequence, prePrepare.View, prePrepare.Digest)

	// Check if we are a backup node
	if consensus.isPrimary() {
		consensus.logger.Debugf("[%s] primary node ignores pre-prepare", consensus.Id)
		return
	}

	// Handle future messages (for sequence ahead of current)
	if prePrepare.Sequence > consensus.Sequence {
		consensus.logger.Infof("[%s] receive future pre-prepare for sequence %d (current: %d)",
			consensus.Id, prePrepare.Sequence, consensus.Sequence)

		// If the sequence is only 1 ahead, we might be lagging behind
		// Check if we should sync to the future sequence
		currentHeight, err := consensus.ledgerCache.CurrentHeight()
		if err == nil && prePrepare.Sequence == currentHeight+1 {
			// The future sequence matches the expected next height
			// This means we should sync our consensus state
			consensus.logger.Infof("[%s] syncing to sequence %d from future pre-prepare",
				consensus.Id, prePrepare.Sequence)
			// Reset state and enter new sequence
			consensus.enterNewSequence(prePrepare.Sequence)
			// Process the pre-prepare message now
			// Continue to process the message below
		} else {
			// Sequence is too far ahead, cannot sync
			consensus.logger.Warnf("[%s] future pre-prepare sequence %d too far ahead (current: %d, ledger: %d), ignoring",
				consensus.Id, prePrepare.Sequence, consensus.Sequence, currentHeight)
			return
		}
	}

	// Check sequence and view
	if prePrepare.Sequence != consensus.Sequence || prePrepare.View != consensus.View {
		consensus.logger.Warnf("[%s] pre-prepare sequence/view mismatch: expected %d/%d, got %d/%d",
			consensus.Id, consensus.Sequence, consensus.View, prePrepare.Sequence, prePrepare.View)
		return
	}

	// Verify primary
	primary, err := consensus.validatorSet.GetPrimary(consensus.View, consensus.Sequence)
	if err != nil || primary != prePrepare.Primary {
		consensus.logger.Warnf("[%s] invalid primary in pre-prepare: %s", consensus.Id, prePrepare.Primary)
		return
	}

	// Verify signature
	if err := consensus.verifyPrePrepare(prePrepare); err != nil {
		consensus.logger.Warnf("[%s] pre-prepare signature verification failed: %v", consensus.Id, err)
		return
	}

	// Check if we already have a PrePrepare for this sequence
	if consensus.PrePrepare != nil {
		if bytes.Equal(consensus.PrePrepare.Digest, prePrepare.Digest) {
			consensus.logger.Debugf("[%s] duplicate pre-prepare", consensus.Id)
			return
		} else {
			consensus.logger.Warnf("[%s] conflicting pre-prepare digest", consensus.Id)
			return
		}
	}

	consensus.PrePrepare = prePrepare
	consensus.Step = pbftpb.PBFTStep_PRE_PREPARE

	// Verify the block
	if prePrepare.Block != nil {
		consensus.logger.Infof("[%s] verifying block from pre-prepare", consensus.Id)
		consensus.msgbus.PublishSafe(msgbus.VerifyBlock, prePrepare.Block)
	} else {
		// No block in pre-prepare, something is wrong
		consensus.logger.Warnf("[%s] pre-prepare without block", consensus.Id)
		consensus.PrePrepare = nil
	}
}

// procPrepare processes a Prepare message
func (consensus *ConsensusPBFTImpl) procPrepare(prepare *pbftpb.Prepare) {
	if prepare == nil {
		return
	}

	consensus.logger.Debugf("[%s](%d/%d/%s) receive prepare from %s (%d/%d/%x)",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step,
		prepare.NodeId, prepare.Sequence, prepare.View, prepare.Digest)

	// Handle future messages
	if prepare.Sequence > consensus.Sequence {
		consensus.logger.Debugf("[%s] receive future prepare for sequence %d (current: %d), caching",
			consensus.Id, prepare.Sequence, consensus.Sequence)

		// Cache future prepare messages
		consensus.futureCacheMutex.Lock()
		if consensus.futurePrepareCache[prepare.Sequence] == nil {
			consensus.futurePrepareCache[prepare.Sequence] = make(map[string]*pbftpb.Prepare)
		}
		// Only cache if not already present (avoid duplicates)
		if _, exists := consensus.futurePrepareCache[prepare.Sequence][prepare.NodeId]; !exists {
			consensus.futurePrepareCache[prepare.Sequence][prepare.NodeId] = prepare
			consensus.logger.Debugf("[%s] cached future prepare from %s for sequence %d",
				consensus.Id, prepare.NodeId, prepare.Sequence)
		}
		consensus.futureCacheMutex.Unlock()
		return
	}

	// Check sequence and view
	if prepare.Sequence != consensus.Sequence || prepare.View != consensus.View {
		consensus.logger.Debugf("[%s] prepare sequence/view mismatch", consensus.Id)
		return
	}

	// Verify validator
	if !consensus.validatorSet.HasValidator(prepare.NodeId) {
		consensus.logger.Warnf("[%s] invalid validator in prepare: %s", consensus.Id, prepare.NodeId)
		return
	}

	// Verify signature
	if err := consensus.verifyPrepare(prepare); err != nil {
		consensus.logger.Warnf("[%s] prepare signature verification failed: %v", consensus.Id, err)
		return
	}

	// Check if PrePrepare exists and digest matches
	if consensus.PrePrepare == nil {
		consensus.logger.Debugf("[%s] prepare received before pre-prepare", consensus.Id)
		return
	}

	if !bytes.Equal(prepare.Digest, consensus.PrePrepare.Digest) {
		consensus.logger.Warnf("[%s] prepare digest mismatch", consensus.Id)
		return
	}

	// Initialize PrepareVoteSet if needed
	if consensus.PrepareVoteSet == nil {
		consensus.PrepareVoteSet = &pbftpb.PrepareVoteSet{
			View:     consensus.View,
			Sequence: consensus.Sequence,
			Digest:   consensus.PrePrepare.Digest,
			Votes:    make(map[string]*pbftpb.Prepare),
		}
	}

	// Add vote
	if _, ok := consensus.PrepareVoteSet.Votes[prepare.NodeId]; ok {
		consensus.logger.Debugf("[%s] duplicate prepare from %s", consensus.Id, prepare.NodeId)
		return
	}

	consensus.PrepareVoteSet.Votes[prepare.NodeId] = prepare
	consensus.PrepareVoteSet.Sum++

	consensus.logger.Infof("[%s](%d/%d/%s) added prepare vote, total: %d",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step, consensus.PrepareVoteSet.Sum)

	// Check if we have 2f+1 prepare votes
	prepareVoteSet := NewPrepareVoteSet(consensus.logger, consensus.View, consensus.Sequence,
		consensus.PrePrepare.Digest, consensus.validatorSet)
	for _, v := range consensus.PrepareVoteSet.Votes {
		prepareVoteSet.AddVote(v)
	}

	if prepareVoteSet.HasTwoThirdsMajority() {
		consensus.logger.Infof("[%s] received 2f+1 prepare votes, entering commit phase",
			consensus.Id)
		consensus.enterCommit()
	} else {
		// Check if we need to set timeout
		if consensus.Step == pbftpb.PBFTStep_PREPARE {
			// Set timeout for prepare phase if not already set
			consensus.timeScheduler.AddTimeoutInfo(pbftpb.TimeoutInfo{
				Duration: consensus.TimeoutPrepare.Nanoseconds(),
				Height:   consensus.Sequence,
				Round:    consensus.View,
				Step:     pbftpb.PBFTStep_PREPARE,
			})
		}
	}
}

// procCommit processes a Commit message
func (consensus *ConsensusPBFTImpl) procCommit(commit *pbftpb.Commit) {
	if commit == nil {
		return
	}

	consensus.logger.Debugf("[%s](%d/%d/%s) receive commit from %s (%d/%d/%x)",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step,
		commit.NodeId, commit.Sequence, commit.View, commit.Digest)

	// Handle future messages
	if commit.Sequence > consensus.Sequence {
		consensus.logger.Debugf("[%s] receive future commit for sequence %d (current: %d), caching",
			consensus.Id, commit.Sequence, consensus.Sequence)

		// Cache future commit messages
		consensus.futureCacheMutex.Lock()
		if consensus.futureCommitCache[commit.Sequence] == nil {
			consensus.futureCommitCache[commit.Sequence] = make(map[string]*pbftpb.Commit)
		}
		// Only cache if not already present (avoid duplicates)
		if _, exists := consensus.futureCommitCache[commit.Sequence][commit.NodeId]; !exists {
			consensus.futureCommitCache[commit.Sequence][commit.NodeId] = commit
			consensus.logger.Debugf("[%s] cached future commit from %s for sequence %d",
				consensus.Id, commit.NodeId, commit.Sequence)
		}
		consensus.futureCacheMutex.Unlock()
		return
	}

	// Check sequence and view
	if commit.Sequence != consensus.Sequence || commit.View != consensus.View {
		consensus.logger.Debugf("[%s] commit sequence/view mismatch", consensus.Id)
		return
	}

	// Verify validator
	if !consensus.validatorSet.HasValidator(commit.NodeId) {
		consensus.logger.Warnf("[%s] invalid validator in commit: %s", consensus.Id, commit.NodeId)
		return
	}

	// Verify signature
	if err := consensus.verifyCommit(commit); err != nil {
		consensus.logger.Warnf("[%s] commit signature verification failed: %v", consensus.Id, err)
		return
	}

	// Check if PrePrepare exists and digest matches
	if consensus.PrePrepare == nil {
		consensus.logger.Debugf("[%s] commit received before pre-prepare", consensus.Id)
		return
	}

	if !bytes.Equal(commit.Digest, consensus.PrePrepare.Digest) {
		consensus.logger.Warnf("[%s] commit digest mismatch", consensus.Id)
		return
	}

	// Initialize CommitVoteSet if needed
	if consensus.CommitVoteSet == nil {
		consensus.CommitVoteSet = &pbftpb.CommitVoteSet{
			View:     consensus.View,
			Sequence: consensus.Sequence,
			Digest:   consensus.PrePrepare.Digest,
			Votes:    make(map[string]*pbftpb.Commit),
		}
	}

	// Add vote
	if _, ok := consensus.CommitVoteSet.Votes[commit.NodeId]; ok {
		consensus.logger.Debugf("[%s] duplicate commit from %s", consensus.Id, commit.NodeId)
		return
	}

	consensus.CommitVoteSet.Votes[commit.NodeId] = commit
	consensus.CommitVoteSet.Sum++

	consensus.logger.Infof("[%s](%d/%d/%s) added commit vote, total: %d",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step, consensus.CommitVoteSet.Sum)

	// Check if we have 2f+1 commit votes
	commitVoteSet := NewCommitVoteSet(consensus.logger, consensus.View, consensus.Sequence,
		consensus.PrePrepare.Digest, consensus.validatorSet)
	for _, v := range consensus.CommitVoteSet.Votes {
		commitVoteSet.AddVote(v)
	}

	if commitVoteSet.HasTwoThirdsMajority() {
		consensus.logger.Infof("[%s] received 2f+1 commit votes, committing block",
			consensus.Id)
		consensus.enterCommitted()
	} else {
		// Check if we need to set timeout
		if consensus.Step == pbftpb.PBFTStep_COMMIT {
			// Set timeout for commit phase if not already set
			consensus.timeScheduler.AddTimeoutInfo(pbftpb.TimeoutInfo{
				Duration: consensus.TimeoutCommit.Nanoseconds(),
				Height:   consensus.Sequence,
				Round:    consensus.View,
				Step:     pbftpb.PBFTStep_COMMIT,
			})
		}
	}
}

// procViewChange processes a ViewChange message
func (consensus *ConsensusPBFTImpl) procViewChange(viewChange *pbftpb.ViewChange) {
	if viewChange == nil {
		return
	}

	consensus.logger.Infof("[%s](%d/%d/%s) receive view-change from %s (curView:%d, nextView:%d)",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step,
		viewChange.NodeId, viewChange.CurView, viewChange.NextView)

	// Check if we are in view change phase
	if consensus.Step != pbftpb.PBFTStep_VIEW_CHANGE {
		consensus.logger.Debugf("[%s] not in view change phase, ignoring view-change", consensus.Id)
		return
	}

	// Check view
	if viewChange.NextView != consensus.View {
		consensus.logger.Debugf("[%s] view-change for different view: expected %d, got %d",
			consensus.Id, consensus.View, viewChange.NextView)
		return
	}

	// Verify validator
	if !consensus.validatorSet.HasValidator(viewChange.NodeId) {
		consensus.logger.Warnf("[%s] invalid validator in view-change: %s", consensus.Id, viewChange.NodeId)
		return
	}

	// Verify signature
	if err := consensus.verifyViewChange(viewChange); err != nil {
		consensus.logger.Warnf("[%s] view-change signature verification failed: %v", consensus.Id, err)
		return
	}

	// Add view change vote
	if consensus.ViewChangeVotes == nil {
		consensus.ViewChangeVotes = make(map[string]*pbftpb.ViewChange)
	}

	if _, ok := consensus.ViewChangeVotes[viewChange.NodeId]; ok {
		consensus.logger.Debugf("[%s] duplicate view-change from %s", consensus.Id, viewChange.NodeId)
		return
	}

	consensus.ViewChangeVotes[viewChange.NodeId] = viewChange

	consensus.logger.Infof("[%s](%d/%d/%s) added view-change vote, total: %d",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step, len(consensus.ViewChangeVotes))

	// Check if we have 2f+1 view change votes
	if len(consensus.ViewChangeVotes) >= int(consensus.validatorSet.Size()*2/3+1) {
		consensus.logger.Infof("[%s] received 2f+1 view-change votes, checking if we are new primary",
			consensus.Id)

		// Check if we are the new primary
		newPrimary, err := consensus.validatorSet.GetPrimary(consensus.View, consensus.Sequence)
		if err != nil {
			consensus.logger.Errorf("[%s] failed to get primary for view %d: %v", consensus.Id, consensus.View, err)
			return
		}

		if newPrimary == consensus.Id {
			// We are the new primary, create and broadcast NewView message
			consensus.createAndBroadcastNewView()
		}
	}
}

// procNewView processes a NewView message
func (consensus *ConsensusPBFTImpl) procNewView(newView *pbftpb.NewView) {
	if newView == nil {
		return
	}

	consensus.logger.Infof("[%s](%d/%d/%s) receive new-view from %s (curView:%d, nextView:%d)",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step,
		newView.NodeId, newView.CurView, newView.NextView)

	// Check if we are in view change phase
	if consensus.Step != pbftpb.PBFTStep_VIEW_CHANGE {
		consensus.logger.Debugf("[%s] not in view change phase, ignoring new-view", consensus.Id)
		return
	}

	// Check view
	if newView.NextView != consensus.View {
		consensus.logger.Debugf("[%s] new-view for different view: expected %d, got %d",
			consensus.Id, consensus.View, newView.NextView)
		return
	}

	// Verify new primary
	newPrimary, err := consensus.validatorSet.GetPrimary(consensus.View, consensus.Sequence)
	if err != nil || newPrimary != newView.NodeId {
		consensus.logger.Warnf("[%s] invalid primary in new-view: expected %s, got %s",
			consensus.Id, newPrimary, newView.NodeId)
		return
	}

	// Verify signature
	if err := consensus.verifyNewView(newView); err != nil {
		consensus.logger.Warnf("[%s] new-view signature verification failed: %v", consensus.Id, err)
		return
	}

	// Verify view change messages
	if len(newView.ViewChangeMessages) < int(consensus.validatorSet.Size()*2/3+1) {
		consensus.logger.Warnf("[%s] new-view has insufficient view-change messages: %d",
			consensus.Id, len(newView.ViewChangeMessages))
		return
	}

	// Verify all view change messages
	for _, vc := range newView.ViewChangeMessages {
		if err := consensus.verifyViewChange(vc); err != nil {
			consensus.logger.Warnf("[%s] invalid view-change in new-view from %s: %v",
				consensus.Id, vc.NodeId, err)
			return
		}
	}

	// Add new view vote
	if consensus.NewViewVotes == nil {
		consensus.NewViewVotes = make(map[string]*pbftpb.NewView)
	}

	if _, ok := consensus.NewViewVotes[newView.NodeId]; ok {
		consensus.logger.Debugf("[%s] duplicate new-view from %s", consensus.Id, newView.NodeId)
		return
	}

	consensus.NewViewVotes[newView.NodeId] = newView

	consensus.logger.Infof("[%s](%d/%d/%s) added new-view vote, total: %d",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step, len(consensus.NewViewVotes))

	// Check if we have 2f+1 new view votes (only need one from primary)
	if len(consensus.NewViewVotes) >= 1 {
		// Enter new view
		consensus.enterNewView(newView)
	}
}

// enterPrepare enters the Prepare phase
func (consensus *ConsensusPBFTImpl) enterPrepare() {
	if consensus.PrePrepare == nil {
		consensus.logger.Warnf("[%s] cannot enter prepare without pre-prepare", consensus.Id)
		return
	}

	consensus.logger.Infof("[%s](%d/%d/%s) enter prepare phase",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step)

	consensus.Step = pbftpb.PBFTStep_PREPARE

	// Initialize PrepareVoteSet
	if consensus.PrepareVoteSet == nil {
		consensus.PrepareVoteSet = &pbftpb.PrepareVoteSet{
			View:     consensus.View,
			Sequence: consensus.Sequence,
			Digest:   consensus.PrePrepare.Digest,
			Votes:    make(map[string]*pbftpb.Prepare),
		}
	}

	// Create and sign Prepare message
	prepare := &pbftpb.Prepare{
		NodeId:   consensus.Id,
		View:     consensus.View,
		Sequence: consensus.Sequence,
		Digest:   consensus.PrePrepare.Digest,
	}

	if err := consensus.signPrepare(prepare); err != nil {
		consensus.logger.Errorf("[%s] sign prepare failed: %v", consensus.Id, err)
		return
	}

	// Add our own vote
	consensus.PrepareVoteSet.Votes[consensus.Id] = prepare
	consensus.PrepareVoteSet.Sum++

	// Save to WAL if enabled
	if consensus.walWriteMode != wal_service.NonWalWrite {
		prepareVoteSetData := mustMarshal(consensus.PrepareVoteSet)
		consensus.saveWalEntry(pbftpb.WalEntryType_PREPARE_ENTRY, consensus.Sequence, prepareVoteSetData)
	}

	// Broadcast Prepare message
	consensus.sendConsensusPrepare(prepare, "")

	// Check if we already have 2f+1 votes
	prepareVoteSet := NewPrepareVoteSet(consensus.logger, consensus.View, consensus.Sequence,
		consensus.PrePrepare.Digest, consensus.validatorSet)
	for _, v := range consensus.PrepareVoteSet.Votes {
		prepareVoteSet.AddVote(v)
	}

	if prepareVoteSet.HasTwoThirdsMajority() {
		consensus.logger.Infof("[%s] already have 2f+1 prepare votes, entering commit phase",
			consensus.Id)
		consensus.enterCommit()
	} else {
		// Set timeout for prepare phase
		// In PBFT, Height stores Sequence, Round stores View
		consensus.timeScheduler.AddTimeoutInfo(pbftpb.TimeoutInfo{
			Duration: consensus.TimeoutPrepare.Nanoseconds(),
			Height:   consensus.Sequence,
			Round:    consensus.View,
			Step:     pbftpb.PBFTStep_PREPARE,
		})
	}
}

// enterCommit enters the Commit phase
func (consensus *ConsensusPBFTImpl) enterCommit() {
	if consensus.PrepareVoteSet == nil || !consensus.hasPrepareQuorum() {
		consensus.logger.Warnf("[%s] cannot enter commit without prepare quorum", consensus.Id)
		return
	}

	consensus.logger.Infof("[%s](%d/%d/%s) enter commit phase",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step)

	consensus.Step = pbftpb.PBFTStep_COMMIT

	// Initialize CommitVoteSet
	if consensus.CommitVoteSet == nil {
		consensus.CommitVoteSet = &pbftpb.CommitVoteSet{
			View:     consensus.View,
			Sequence: consensus.Sequence,
			Digest:   consensus.PrePrepare.Digest,
			Votes:    make(map[string]*pbftpb.Commit),
		}
	}

	// Create and sign Commit message
	commit := &pbftpb.Commit{
		NodeId:   consensus.Id,
		View:     consensus.View,
		Sequence: consensus.Sequence,
		Digest:   consensus.PrePrepare.Digest,
	}

	if err := consensus.signCommit(commit); err != nil {
		consensus.logger.Errorf("[%s] sign commit failed: %v", consensus.Id, err)
		return
	}

	// Add our own vote
	consensus.CommitVoteSet.Votes[consensus.Id] = commit
	consensus.CommitVoteSet.Sum++

	// Broadcast Commit message
	consensus.sendConsensusCommit(commit, "")

	// Check if we already have 2f+1 votes
	commitVoteSet := NewCommitVoteSet(consensus.logger, consensus.View, consensus.Sequence,
		consensus.PrePrepare.Digest, consensus.validatorSet)
	for _, v := range consensus.CommitVoteSet.Votes {
		commitVoteSet.AddVote(v)
	}

	if commitVoteSet.HasTwoThirdsMajority() {
		consensus.logger.Infof("[%s] already have 2f+1 commit votes, committing block",
			consensus.Id)
		consensus.enterCommitted()
	} else {
		// Set timeout for commit phase
		// In PBFT, Height stores Sequence, Round stores View
		consensus.timeScheduler.AddTimeoutInfo(pbftpb.TimeoutInfo{
			Duration: consensus.TimeoutCommit.Nanoseconds(),
			Height:   consensus.Sequence,
			Round:    consensus.View,
			Step:     pbftpb.PBFTStep_COMMIT,
		})
	}
}

// enterCommitted commits the block
func (consensus *ConsensusPBFTImpl) enterCommitted() {
	if consensus.CommitVoteSet == nil || !consensus.hasCommitQuorum() {
		consensus.logger.Warnf("[%s] cannot commit without commit quorum", consensus.Id)
		return
	}

	if consensus.PrePrepare == nil || consensus.PrePrepare.Block == nil {
		consensus.logger.Warnf("[%s] cannot commit without block", consensus.Id)
		return
	}

	consensus.logger.Infof("[%s](%d/%d/%s) enter committed phase",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step)

	consensus.Step = pbftpb.PBFTStep_COMMITTED

	// Commit the block
	consensus.commitBlock(consensus.PrePrepare.Block, consensus.CommitVoteSet)
}

// enterNewSequence enters a new sequence (height)
func (consensus *ConsensusPBFTImpl) enterNewSequence(sequence uint64) {
	if consensus.Sequence >= sequence {
		consensus.logger.Debugf("[%s] invalid new sequence: current=%d, new=%d",
			consensus.Id, consensus.Sequence, sequence)
		return
	}

	consensus.logger.Infof("[%s] enter new sequence: %d (from %d)",
		consensus.Id, sequence, consensus.Sequence)

	// Save current state to cache
	if consensus.Sequence > 0 {
		consensus.consensusStateCache.addConsensusState(consensus.ConsensusState)
	}

	// Update chain config
	addedValidators, removedValidators, err := consensus.updateChainConfig()
	if err != nil {
		consensus.logger.Errorf("[%s](%d/%d/%s) update chain config failed: %v",
			consensus.Id, consensus.Sequence, consensus.View, consensus.Step, err)
	} else {
		if len(addedValidators) > 0 || len(removedValidators) > 0 {
			consensus.logger.Infof("[%s] validators updated: added=%v, removed=%v",
				consensus.Id, addedValidators, removedValidators)
		}
	}

	// Reset state for new sequence
	consensus.Sequence = sequence
	consensus.Step = pbftpb.PBFTStep_NEW_HEIGHT
	consensus.PrePrepare = nil
	consensus.PrepareVoteSet = nil
	consensus.CommitVoteSet = nil
	consensus.ViewChangeVotes = make(map[string]*pbftpb.ViewChange)
	consensus.NewViewVotes = make(map[string]*pbftpb.NewView)

	// Update block version
	consensus.blockVersion = consensus.chainConf.ChainConfig().GetBlockVersion()

	// Replay cached future messages for this sequence
	consensus.replayCachedMessages(sequence)

	// Send propose state to core engine
	consensus.sendProposeState(consensus.isPrimary())
}

// replayCachedMessages replays cached future prepare and commit messages for the given sequence
// Note: This function should be called while holding the main consensus lock (via enterNewSequence)
func (consensus *ConsensusPBFTImpl) replayCachedMessages(sequence uint64) {
	// Collect cached messages first (with cache lock)
	prepareMessages := make([]*pbftpb.Prepare, 0)
	commitMessages := make([]*pbftpb.Commit, 0)

	consensus.futureCacheMutex.Lock()
	// Collect prepare messages
	if prepareCache, exists := consensus.futurePrepareCache[sequence]; exists && len(prepareCache) > 0 {
		for _, prepare := range prepareCache {
			prepareMessages = append(prepareMessages, prepare)
		}
		delete(consensus.futurePrepareCache, sequence)
	}
	// Collect commit messages
	if commitCache, exists := consensus.futureCommitCache[sequence]; exists && len(commitCache) > 0 {
		for _, commit := range commitCache {
			commitMessages = append(commitMessages, commit)
		}
		delete(consensus.futureCommitCache, sequence)
	}
	// Clean up old cache entries (keep only sequences within reasonable range)
	maxCacheSequence := sequence + 10 // Keep cache for sequences up to 10 ahead
	for seq := range consensus.futurePrepareCache {
		if seq < sequence || seq > maxCacheSequence {
			delete(consensus.futurePrepareCache, seq)
		}
	}
	for seq := range consensus.futureCommitCache {
		if seq < sequence || seq > maxCacheSequence {
			delete(consensus.futureCommitCache, seq)
		}
	}
	consensus.futureCacheMutex.Unlock()

	// Replay cached prepare messages (without cache lock, but main lock should be held by caller)
	if len(prepareMessages) > 0 {
		consensus.logger.Infof("[%s] replaying %d cached prepare messages for sequence %d",
			consensus.Id, len(prepareMessages), sequence)
		for _, prepare := range prepareMessages {
			consensus.logger.Debugf("[%s] replaying cached prepare from %s for sequence %d",
				consensus.Id, prepare.NodeId, sequence)
			consensus.procPrepare(prepare)
		}
	}

	// Replay cached commit messages
	if len(commitMessages) > 0 {
		consensus.logger.Infof("[%s] replaying %d cached commit messages for sequence %d",
			consensus.Id, len(commitMessages), sequence)
		for _, commit := range commitMessages {
			consensus.logger.Debugf("[%s] replaying cached commit from %s for sequence %d",
				consensus.Id, commit.NodeId, sequence)
			consensus.procCommit(commit)
		}
	}
}

// updateChainConfig updates chain configuration and returns added/removed validators
func (consensus *ConsensusPBFTImpl) updateChainConfig() (addedValidators []string, removedValidators []string,
	err error) {
	consensus.logger.Debugf("[%s](%d/%d/%s) update chain consensusConfig",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step)

	consensusConfig := consensus.chainConf.ChainConfig().Consensus
	validators, timeoutPrePrepare, timeoutPrepare, timeoutCommit, timeoutViewChange, err :=
		consensus.extractConsensusConfig(consensusConfig)
	if err != nil {
		return nil, nil, err
	}

	// Update timeout configurations
	consensus.TimeoutPrePrepare = timeoutPrePrepare
	consensus.TimeoutPrepare = timeoutPrepare
	consensus.TimeoutCommit = timeoutCommit
	consensus.TimeoutViewChange = timeoutViewChange

	consensus.logger.Debugf("[%s](%d/%d/%s) update chain consensusConfig, TimeoutPrePrepare: %v, "+
		"TimeoutPrepare: %v, TimeoutCommit: %v, TimeoutViewChange: %v, validators: %v",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step,
		consensus.TimeoutPrePrepare, consensus.TimeoutPrepare, consensus.TimeoutCommit,
		consensus.TimeoutViewChange, validators)

	return consensus.validatorSet.updateValidators(validators)
}

// enterViewChange enters view change phase
func (consensus *ConsensusPBFTImpl) enterViewChange() {
	consensus.logger.Infof("[%s](%d/%d/%s) enter view change",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step)

	newView := consensus.View + 1
	consensus.View = newView
	consensus.Step = pbftpb.PBFTStep_VIEW_CHANGE

	// Reset state
	consensus.PrePrepare = nil
	consensus.PrepareVoteSet = nil
	consensus.CommitVoteSet = nil
	consensus.ViewChangeVotes = make(map[string]*pbftpb.ViewChange)
	consensus.NewViewVotes = make(map[string]*pbftpb.NewView)

	// Create and broadcast ViewChange message
	viewChange := &pbftpb.ViewChange{
		NodeId:   consensus.Id,
		CurView:  consensus.View - 1,
		NextView: consensus.View,
		Sequence: consensus.Sequence,
	}

	if err := consensus.signViewChange(viewChange); err != nil {
		consensus.logger.Errorf("[%s] sign view-change failed: %v", consensus.Id, err)
		return
	}

	// Add our own view change vote
	consensus.ViewChangeVotes[consensus.Id] = viewChange

	// Broadcast ViewChange message
	consensus.sendConsensusViewChange(viewChange, "")

	consensus.logger.Infof("[%s](%d/%d/%s) broadcasted view-change message",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step)

	// Set timeout for view change
	// In PBFT, Height stores Sequence, Round stores View
	consensus.timeScheduler.AddTimeoutInfo(pbftpb.TimeoutInfo{
		Duration: consensus.TimeoutViewChange.Nanoseconds(),
		Height:   consensus.Sequence,
		Round:    consensus.View,
		Step:     pbftpb.PBFTStep_VIEW_CHANGE,
	})
}

// createAndBroadcastNewView creates and broadcasts a NewView message
func (consensus *ConsensusPBFTImpl) createAndBroadcastNewView() {
	if len(consensus.ViewChangeVotes) < int(consensus.validatorSet.Size()*2/3+1) {
		consensus.logger.Warnf("[%s] insufficient view-change votes for new-view: %d",
			consensus.Id, len(consensus.ViewChangeVotes))
		return
	}

	// Collect view change messages
	viewChangeMessages := make([]*pbftpb.ViewChange, 0, len(consensus.ViewChangeVotes))
	for _, vc := range consensus.ViewChangeVotes {
		viewChangeMessages = append(viewChangeMessages, vc)
	}

	// Create NewView message
	newView := &pbftpb.NewView{
		NodeId:             consensus.Id,
		CurView:            consensus.View - 1,
		NextView:           consensus.View,
		NewSequence:        consensus.Sequence,
		ViewChangeMessages: viewChangeMessages,
	}

	if err := consensus.signNewView(newView); err != nil {
		consensus.logger.Errorf("[%s] sign new-view failed: %v", consensus.Id, err)
		return
	}

	consensus.logger.Infof("[%s](%d/%d/%s) created new-view message with %d view-change messages",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step, len(viewChangeMessages))

	// Broadcast NewView message
	consensus.sendConsensusNewView(newView, "")

	// Add our own new view vote
	if consensus.NewViewVotes == nil {
		consensus.NewViewVotes = make(map[string]*pbftpb.NewView)
	}
	consensus.NewViewVotes[consensus.Id] = newView

	// Enter new view
	consensus.enterNewView(newView)
}

// enterNewView enters a new view after receiving NewView message
func (consensus *ConsensusPBFTImpl) enterNewView(newView *pbftpb.NewView) {
	consensus.logger.Infof("[%s](%d/%d/%s) enter new view",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step)

	// Update view
	consensus.View = newView.NextView
	consensus.Step = pbftpb.PBFTStep_NEW_HEIGHT

	// Reset state for new view
	consensus.PrePrepare = nil
	consensus.PrepareVoteSet = nil
	consensus.CommitVoteSet = nil
	consensus.ViewChangeVotes = make(map[string]*pbftpb.ViewChange)
	consensus.NewViewVotes = make(map[string]*pbftpb.NewView)

	// Send propose state to core engine
	consensus.sendProposeState(consensus.isPrimary())
}

// hasPrepareQuorum checks if we have 2f+1 prepare votes
func (consensus *ConsensusPBFTImpl) hasPrepareQuorum() bool {
	if consensus.PrepareVoteSet == nil {
		return false
	}
	prepareVoteSet := NewPrepareVoteSet(consensus.logger, consensus.View, consensus.Sequence,
		consensus.PrePrepare.Digest, consensus.validatorSet)
	for _, v := range consensus.PrepareVoteSet.Votes {
		prepareVoteSet.AddVote(v)
	}
	return prepareVoteSet.HasTwoThirdsMajority()
}

// hasCommitQuorum checks if we have 2f+1 commit votes
func (consensus *ConsensusPBFTImpl) hasCommitQuorum() bool {
	if consensus.CommitVoteSet == nil {
		return false
	}
	commitVoteSet := NewCommitVoteSet(consensus.logger, consensus.View, consensus.Sequence,
		consensus.PrePrepare.Digest, consensus.validatorSet)
	for _, v := range consensus.CommitVoteSet.Votes {
		commitVoteSet.AddVote(v)
	}
	return commitVoteSet.HasTwoThirdsMajority()
}

// verifyPrePrepare verifies a PrePrepare message signature
func (consensus *ConsensusPBFTImpl) verifyPrePrepare(prePrepare *pbftpb.PrePrepare) error {
	if prePrepare.Endorsement == nil {
		return fmt.Errorf("pre-prepare without endorsement")
	}

	prePrepareCopy := &pbftpb.PrePrepare{
		Primary:  prePrepare.Primary,
		View:     prePrepare.View,
		Sequence: prePrepare.Sequence,
		Digest:   prePrepare.Digest,
		Block:    prePrepare.Block,
		TxsRwSet: prePrepare.TxsRwSet,
	}
	message := mustMarshal(prePrepareCopy)

	principal, err := consensus.ac.CreatePrincipal(
		protocol.ResourceNameConsensusNode,
		[]*common.EndorsementEntry{prePrepare.Endorsement},
		message,
	)
	if err != nil {
		return err
	}

	result, err := consensus.ac.VerifyMsgPrincipal(principal, consensus.blockVersion)
	if err != nil {
		return err
	}

	if !result {
		return fmt.Errorf("pre-prepare signature verification failed")
	}

	return nil
}

// verifyPrepare verifies a Prepare message signature
func (consensus *ConsensusPBFTImpl) verifyPrepare(prepare *pbftpb.Prepare) error {
	if prepare.Endorsement == nil {
		return fmt.Errorf("prepare without endorsement")
	}

	prepareCopy := &pbftpb.Prepare{
		NodeId:   prepare.NodeId,
		View:     prepare.View,
		Sequence: prepare.Sequence,
		Digest:   prepare.Digest,
	}
	message := mustMarshal(prepareCopy)

	principal, err := consensus.ac.CreatePrincipal(
		protocol.ResourceNameConsensusNode,
		[]*common.EndorsementEntry{prepare.Endorsement},
		message,
	)
	if err != nil {
		return err
	}

	result, err := consensus.ac.VerifyMsgPrincipal(principal, consensus.blockVersion)
	if err != nil {
		return err
	}

	if !result {
		return fmt.Errorf("prepare signature verification failed")
	}

	return nil
}

// verifyCommit verifies a Commit message signature
func (consensus *ConsensusPBFTImpl) verifyCommit(commit *pbftpb.Commit) error {
	if commit.Endorsement == nil {
		return fmt.Errorf("commit without endorsement")
	}

	commitCopy := &pbftpb.Commit{
		NodeId:   commit.NodeId,
		View:     commit.View,
		Sequence: commit.Sequence,
		Digest:   commit.Digest,
	}
	message := mustMarshal(commitCopy)

	principal, err := consensus.ac.CreatePrincipal(
		protocol.ResourceNameConsensusNode,
		[]*common.EndorsementEntry{commit.Endorsement},
		message,
	)
	if err != nil {
		return err
	}

	result, err := consensus.ac.VerifyMsgPrincipal(principal, consensus.blockVersion)
	if err != nil {
		return err
	}

	if !result {
		return fmt.Errorf("commit signature verification failed")
	}

	return nil
}

// commitBlock commits a block to the core engine
func (consensus *ConsensusPBFTImpl) commitBlock(block *common.Block, commitVoteSet *pbftpb.CommitVoteSet) {
	consensus.logger.Infof("[%s] commitBlock to %d-%x",
		consensus.Id, block.Header.BlockHeight, block.Header.BlockHash)

	if block.AdditionalData == nil || block.AdditionalData.ExtraData == nil {
		block.AdditionalData = &common.AdditionalData{
			ExtraData: make(map[string][]byte),
		}
	}

	// Store commit votes in block additional data
	qc := mustMarshal(commitVoteSet)
	block.AdditionalData.ExtraData[PBFTAdditionalDataKey] = qc

	// Save to WAL if enabled
	if consensus.walWriteMode != wal_service.NonWalWrite {
		consensus.saveWalEntry(pbftpb.WalEntryType_COMMIT_ENTRY, consensus.Sequence, qc)
	}

	consensus.msgbus.Publish(msgbus.CommitBlock, block)

	consensus.logger.Infof("[%s](%d/%d/%s) consensus commit block (%d/%x)",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step,
		block.Header.BlockHeight, block.Header.BlockHash)
}

// GetValidators returns the list of validators
func (consensus *ConsensusPBFTImpl) GetValidators() ([]string, error) {
	if atomic.LoadInt32(&consensus.state) != start {
		return nil, fmt.Errorf("the node is not a consensus node consensus.state : %d",
			atomic.LoadInt32(&consensus.state))
	}
	consensus.RLock()
	defer consensus.RUnlock()
	return consensus.validatorSet.Validators, nil
}

// GetLastHeight returns the current sequence (height) from consensus state
func (consensus *ConsensusPBFTImpl) GetLastHeight() uint64 {
	if atomic.LoadInt32(&consensus.state) != start {
		return math.MaxUint64
	}
	consensus.RLock()
	defer consensus.RUnlock()
	return consensus.Sequence
}

// GetConsensusStateJSON returns consensus state in JSON format
func (consensus *ConsensusPBFTImpl) GetConsensusStateJSON() ([]byte, error) {
	if atomic.LoadInt32(&consensus.state) != start {
		return nil, fmt.Errorf("the consensus engine of this node is not started")
	}
	consensus.RLock()
	defer consensus.RUnlock()
	cs := consensus.ConsensusState.toProto()
	return mustMarshal(cs), nil
}

// ToProto returns the consensus state as protobuf message
func (consensus *ConsensusPBFTImpl) ToProto() *pbftpb.ConsensusState {
	consensus.RLock()
	defer consensus.RUnlock()
	msg := proto.Clone(consensus.ConsensusState.toProto())
	return msg.(*pbftpb.ConsensusState)
}

// saveWalEntry saves an entry to WAL
func (consensus *ConsensusPBFTImpl) saveWalEntry(entryType pbftpb.WalEntryType, sequence uint64, data []byte) {
	if consensus.walWriteMode == wal_service.NonWalWrite {
		return
	}

	entry := &pbftpb.WalEntry{
		Type:   entryType,
		Height: sequence,
		Data:   data,
	}

	entryData := mustMarshal(entry)
	if err := consensus.wal.Write(int8(consensus.walWriteMode), entryData); err != nil {
		consensus.logger.Errorf("[%s] failed to write wal entry: %v", consensus.Id, err)
	}
}

// signViewChange signs a ViewChange message
func (consensus *ConsensusPBFTImpl) signViewChange(viewChange *pbftpb.ViewChange) error {
	viewChangeCopy := &pbftpb.ViewChange{
		NodeId:   viewChange.NodeId,
		CurView:  viewChange.CurView,
		NextView: viewChange.NextView,
		Sequence: viewChange.Sequence,
	}
	viewChangeBz := mustMarshal(viewChangeCopy)

	sig, err := consensus.signer.Sign(consensus.chainConf.ChainConfig().Crypto.Hash, viewChangeBz)
	if err != nil {
		return err
	}

	serializeMember, err := consensus.signer.GetMember()
	if err != nil {
		return err
	}

	viewChange.Endorsement = &common.EndorsementEntry{
		Signer:    serializeMember,
		Signature: sig,
	}
	return nil
}

// signNewView signs a NewView message
func (consensus *ConsensusPBFTImpl) signNewView(newView *pbftpb.NewView) error {
	// Create a copy without endorsement for signing
	newViewCopy := &pbftpb.NewView{
		NodeId:             newView.NodeId,
		CurView:            newView.CurView,
		NextView:           newView.NextView,
		NewSequence:        newView.NewSequence,
		ViewChangeMessages: newView.ViewChangeMessages,
	}
	newViewBz := mustMarshal(newViewCopy)

	sig, err := consensus.signer.Sign(consensus.chainConf.ChainConfig().Crypto.Hash, newViewBz)
	if err != nil {
		return err
	}

	serializeMember, err := consensus.signer.GetMember()
	if err != nil {
		return err
	}

	newView.Endorsement = &common.EndorsementEntry{
		Signer:    serializeMember,
		Signature: sig,
	}
	return nil
}

// verifyViewChange verifies a ViewChange message signature
func (consensus *ConsensusPBFTImpl) verifyViewChange(viewChange *pbftpb.ViewChange) error {
	if viewChange.Endorsement == nil {
		return fmt.Errorf("view-change without endorsement")
	}

	viewChangeCopy := &pbftpb.ViewChange{
		NodeId:   viewChange.NodeId,
		CurView:  viewChange.CurView,
		NextView: viewChange.NextView,
		Sequence: viewChange.Sequence,
	}
	message := mustMarshal(viewChangeCopy)

	principal, err := consensus.ac.CreatePrincipal(
		protocol.ResourceNameConsensusNode,
		[]*common.EndorsementEntry{viewChange.Endorsement},
		message,
	)
	if err != nil {
		return err
	}

	result, err := consensus.ac.VerifyMsgPrincipal(principal, consensus.blockVersion)
	if err != nil {
		return err
	}

	if !result {
		return fmt.Errorf("view-change signature verification failed")
	}

	return nil
}

// verifyNewView verifies a NewView message signature
func (consensus *ConsensusPBFTImpl) verifyNewView(newView *pbftpb.NewView) error {
	if newView.Endorsement == nil {
		return fmt.Errorf("new-view without endorsement")
	}

	newViewCopy := &pbftpb.NewView{
		NodeId:             newView.NodeId,
		CurView:            newView.CurView,
		NextView:           newView.NextView,
		NewSequence:        newView.NewSequence,
		ViewChangeMessages: newView.ViewChangeMessages,
	}
	message := mustMarshal(newViewCopy)

	principal, err := consensus.ac.CreatePrincipal(
		protocol.ResourceNameConsensusNode,
		[]*common.EndorsementEntry{newView.Endorsement},
		message,
	)
	if err != nil {
		return err
	}

	result, err := consensus.ac.VerifyMsgPrincipal(principal, consensus.blockVersion)
	if err != nil {
		return err
	}

	if !result {
		return fmt.Errorf("new-view signature verification failed")
	}

	return nil
}

// sendProposeState sends propose state to core engine via msgbus
func (consensus *ConsensusPBFTImpl) sendProposeState(isPrimary bool) {
	consensus.logger.Infof("[%s](%d/%d/%s) sendProposeState isPrimary: %v",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step, isPrimary)
	consensus.msgbus.PublishSafe(msgbus.ProposeState, isPrimary)
}

const (
	start = 1
	stop  = 0
)

var (
	defaultChanCap                 = 1000
	defaultConsensusStateCacheSize = uint64(10)
	PBFTAdditionalDataKey          = "PBFTAdditionalDataKey"
	PBFT_timeout_preprepare_key    = "PBFT_timeout_preprepare"
	PBFT_timeout_prepare_key       = "PBFT_timeout_prepare"
	PBFT_timeout_commit_key        = "PBFT_timeout_commit"
	PBFT_timeout_viewchange_key    = "PBFT_timeout_viewchange"
)

const (
	// DefaultTimeoutPrePrepare timeout for pre-prepare phase
	DefaultTimeoutPrePrepare = 30 * time.Second
	// DefaultTimeoutPrepare timeout for prepare phase
	DefaultTimeoutPrepare = 30 * time.Second
	// DefaultTimeoutCommit timeout for commit phase
	DefaultTimeoutCommit = 30 * time.Second
	// DefaultTimeoutViewChange timeout for view change
	DefaultTimeoutViewChange = 60 * time.Second
)

var msgBusTopics = []msgbus.Topic{msgbus.ProposedBlock, msgbus.VerifyResult,
	msgbus.RecvConsensusMsg, msgbus.BlockInfo}

// ConsensusPBFTImpl is the implementation of PBFT algorithm
// and it implements the ConsensusEngine interface.
type ConsensusPBFTImpl struct {
	sync.RWMutex
	ctx    context.Context
	logger protocol.Logger
	// chain id
	chainID string
	// node id
	Id string
	// Currently nil, not used
	extendHandler protocol.ConsensusExtendHandler
	// signer（node）
	signer protocol.SigningMember
	// sync service
	syncService protocol.SyncService
	// Access Control
	ac protocol.AccessControlProvider
	// Cache the latest block in ledger(wal)
	ledgerCache protocol.LedgerCache
	// block version
	blockVersion uint32
	// chain conf
	chainConf protocol.ChainConf
	// net service
	netService protocol.NetService
	// send/receive a message using msgbus
	msgbus msgbus.MessageBus
	// stop pbft
	closeC chan struct{}
	// wal is used to record the consensus state and prevent forks
	wal *lws.Lws
	// write wal sync: 0
	walWriteMode wal_service.WalWriteMode
	// validator Set
	validatorSet *validatorSet
	// Current Consensus State
	*ConsensusState
	// History Consensus State
	consensusStateCache *consensusStateCache
	// timeScheduler is used by consensus for schedule timeout events
	timeScheduler *timeScheduler

	// channel for processing a block
	proposedBlockC chan *proposedProposal
	// channel used to verify the results
	verifyResultC chan *consensuspb.VerifyResult
	// channel used to enter new height
	blockHeightC chan uint64
	// channel used to externalMsg（msgbus）
	externalMsgC chan *ConsensusMsg

	// Timeout configurations
	TimeoutPrePrepare time.Duration
	TimeoutPrepare    time.Duration
	TimeoutCommit     time.Duration
	TimeoutViewChange time.Duration

	// use block verifier from core module
	blockVerifier protocol.BlockVerifier

	state int32

	// Cache for future prepare messages (sequence -> map[nodeId] -> prepare)
	futurePrepareCache map[uint64]map[string]*pbftpb.Prepare
	// Cache for future commit messages (sequence -> map[nodeId] -> commit)
	futureCommitCache map[uint64]map[string]*pbftpb.Commit
	// Mutex for future message caches
	futureCacheMutex sync.RWMutex
}

// proposedProposal represents a proposed block with its proposal
type proposedProposal struct {
	proposedBlock *consensuspb.ProposalBlock
}

// New creates a pbft consensus instance
func New(config *consensusUtils.ConsensusImplConfig) (*ConsensusPBFTImpl, error) {
	var err error
	consensus := &ConsensusPBFTImpl{}
	consensus.logger = config.Logger
	consensus.logger.Infof("New ConsensusPBFTImpl[%s]", config.NodeId)
	consensus.chainID = config.ChainId
	consensus.Id = config.NodeId
	consensus.signer = config.Signer
	consensus.ac = config.Ac
	consensus.syncService = config.Sync
	consensus.ledgerCache = config.LedgerCache
	consensus.chainConf = config.ChainConf
	consensus.netService = config.NetService
	consensus.msgbus = config.MsgBus
	consensus.closeC = make(chan struct{})

	// init the wal service
	consensus.wal, consensus.walWriteMode, err = InitLWS(config.ChainConf.ChainConfig().Consensus,
		consensus.chainID, consensus.Id)
	if err != nil {
		return nil, err
	}

	consensus.proposedBlockC = make(chan *proposedProposal, defaultChanCap)
	consensus.verifyResultC = make(chan *consensuspb.VerifyResult, defaultChanCap)
	consensus.blockHeightC = make(chan uint64, defaultChanCap)
	consensus.externalMsgC = make(chan *ConsensusMsg, defaultChanCap)
	consensus.blockVerifier = config.Core.GetBlockVerifier()

	validators, err := GetValidatorListFromConfig(consensus.chainConf.ChainConfig())
	if err != nil {
		return nil, err
	}
	consensus.validatorSet = newValidatorSet(consensus.logger, validators)
	consensus.ConsensusState = NewConsensusState(consensus.logger, consensus.Id)
	consensus.consensusStateCache = newConsensusStateCache(defaultConsensusStateCacheSize)
	consensus.timeScheduler = newTimeScheduler(consensus.logger, config.NodeId)
	consensus.futurePrepareCache = make(map[uint64]map[string]*pbftpb.Prepare)
	consensus.futureCommitCache = make(map[uint64]map[string]*pbftpb.Commit)

	// Initialize timeout configurations
	consensus.TimeoutPrePrepare = DefaultTimeoutPrePrepare
	consensus.TimeoutPrepare = DefaultTimeoutPrepare
	consensus.TimeoutCommit = DefaultTimeoutCommit
	consensus.TimeoutViewChange = DefaultTimeoutViewChange

	// Initialize block version
	consensus.blockVersion = consensus.chainConf.ChainConfig().GetBlockVersion()

	consensus.state = stop
	return consensus, nil
}

// Start implements the Start method of ConsensusEngine interface.
func (consensus *ConsensusPBFTImpl) Start() error {
	atomic.StoreInt32(&consensus.state, start)

	go func() {
		// 监听同步模块的同步到理想高度事件
		if consensus.syncService != nil &&
			!(consensus.validatorSet.Size() == 1 && consensus.validatorSet.HasValidator(consensus.Id)) {
			<-consensus.syncService.ListenSyncToIdealHeight()
		}
		consensus.Lock()
		defer consensus.Unlock()
		// 检查共识状态是否为停止状态
		if consensus.state == stop {
			consensus.logger.Infof("ConsensusPBFTImpl[%s] exited during starting because Stop has been called",
				consensus.Id)
			return
		}
		// 注册到消息总线以订阅主题
		for _, topic := range msgBusTopics {
			consensus.msgbus.Register(topic, consensus)
		}
		_ = chainconf.RegisterVerifier(consensus.chainID, consensuspb.ConsensusType_PBFT, consensus)

		consensus.logger.Infof("start ConsensusPBFTImpl[%s]", consensus.Id)
		consensus.timeScheduler.Start()

		err := consensus.replayWal()
		if err != nil {
			consensus.logger.Warnf("replayWal failed, err = %v", err)
			return
		}

		go consensus.handle()
	}()

	return nil
}

// Stop implements the Stop method of ConsensusEngine interface.
func (consensus *ConsensusPBFTImpl) Stop() error {
	consensus.Lock()
	defer consensus.Unlock()

	for _, topic := range msgBusTopics {
		consensus.msgbus.UnRegister(topic, consensus)
	}

	consensus.logger.Infof("[%s](%d/%d/%s) stopped", consensus.Id, consensus.Sequence, consensus.View,
		consensus.Step)
	consensus.wal.Close()
	close(consensus.closeC)
	atomic.StoreInt32(&consensus.state, stop)
	return nil
}

// InitExtendHandler registered extendHandler
func (consensus *ConsensusPBFTImpl) InitExtendHandler(handler protocol.ConsensusExtendHandler) {
	consensus.extendHandler = handler
}

// OnMessage implements the OnMessage method of msgbus.
// OnMessage 处理来自消息总线的消息
func (consensus *ConsensusPBFTImpl) OnMessage(message *msgbus.Message) {
	consensus.logger.Debugf("[%s] OnMessage receive topic: %s", consensus.Id, message.Topic)

	switch message.Topic {
	//核心引擎打包区块
	case msgbus.ProposedBlock:
		if proposedBlock, ok := message.Payload.(*consensuspb.ProposalBlock); ok {
			consensus.proposedBlockC <- &proposedProposal{proposedBlock}
			consensus.logger.Debugf("len of proposedBlockC: %d", len(consensus.proposedBlockC))
		} else {
			consensus.logger.Warnf("assert ProposalBlock failed, get type:{%s}", reflect.TypeOf(message.Payload))
		}
	//核心引擎返回给PBFT Verify的结果
	case msgbus.VerifyResult:
		if verifyResult, ok := message.Payload.(*consensuspb.VerifyResult); ok {
			consensus.logger.Debugf("[%s] verify result: %s", consensus.Id, verifyResult.Code)
			consensus.verifyResultC <- verifyResult
		} else {
			consensus.logger.Warnf("assert VerifyResult failed, get type:{%s}", reflect.TypeOf(message.Payload))
		}
	//核心引擎提交区块到账本
	case msgbus.RecvConsensusMsg:
		if msg, ok := message.Payload.(*netpb.NetMsg); ok {
			if consensusMsg := consensus.createConsensusMsgFromPBFTMsgBz(msg.Payload); consensusMsg != nil {
				consensus.externalMsgC <- consensusMsg
			} else {
				consensus.logger.Warnf("assert Consensus Msg failed")
			}
		} else {
			consensus.logger.Warnf("assert NetMsg failed, get type:{%s}", reflect.TypeOf(message.Payload))
		}
	//核心引擎告知PBFT已提交区块的高度等信息，PBFT进入下一个高度
	case msgbus.BlockInfo:
		if blockInfo, ok := message.Payload.(*common.BlockInfo); ok {
			if blockInfo == nil || blockInfo.Block == nil {
				consensus.logger.Errorf("receive message failed, error message BlockInfo = nil")
				return
			}
			consensus.blockHeightC <- blockInfo.Block.Header.BlockHeight
		} else {
			consensus.logger.Warnf("assert BlockInfo failed, get type:{%s}", reflect.TypeOf(message.Payload))
		}
	}
}

// OnQuit implements the OnQuit method of msgbus.
func (consensus *ConsensusPBFTImpl) OnQuit() {
	consensus.logger.Infof("ConsensusPBFTImpl msgbus OnQuit")
}

// Verify implements interface of struct Verifier,
// This interface is used to verify the validity of parameters,
// it executes before consensus.
func (consensus *ConsensusPBFTImpl) Verify(consensusType consensuspb.ConsensusType,
	chainConfig *config.ChainConfig) error {
	consensus.logger.Infof("[%s](%d/%d/%v) verify chain consensusConfig",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step)
	if consensusType != consensuspb.ConsensusType_PBFT {
		errMsg := fmt.Sprintf("consensus type is not PBFT: %s", consensusType)
		return errors.New(errMsg)
	}
	consensusConfig := chainConfig.Consensus
	_, _, _, _, _, err := consensus.extractConsensusConfig(consensusConfig)
	return err
}

// createConsensusMsgFromPBFTMsgBz creates ConsensusMsg from PBFT message bytes
func (consensus *ConsensusPBFTImpl) createConsensusMsgFromPBFTMsgBz(msgBz []byte) *ConsensusMsg {
	if msgBz == nil || len(msgBz) == 0 {
		return nil
	}

	pbftMsg := &pbftpb.PBFTMsg{}
	if err := proto.Unmarshal(msgBz, pbftMsg); err != nil {
		consensus.logger.Warnf("unmarshal PBFTMsg failed: %v", err)
		return nil
	}

	consensusMsg := &ConsensusMsg{
		Type: pbftMsg.Type,
	}

	switch pbftMsg.Type {
	case pbftpb.PBFTMsgType_MSG_PREPREPARE:
		prePrepare := &pbftpb.PrePrepare{}
		if err := proto.Unmarshal(pbftMsg.Msg, prePrepare); err != nil {
			consensus.logger.Warnf("unmarshal PrePrepare failed: %v", err)
			return nil
		}
		consensusMsg.Msg = prePrepare
	case pbftpb.PBFTMsgType_MSG_PREPARE:
		prepare := &pbftpb.Prepare{}
		if err := proto.Unmarshal(pbftMsg.Msg, prepare); err != nil {
			consensus.logger.Warnf("unmarshal Prepare failed: %v", err)
			return nil
		}
		consensusMsg.Msg = prepare
	case pbftpb.PBFTMsgType_MSG_COMMIT:
		commit := &pbftpb.Commit{}
		if err := proto.Unmarshal(pbftMsg.Msg, commit); err != nil {
			consensus.logger.Warnf("unmarshal Commit failed: %v", err)
			return nil
		}
		consensusMsg.Msg = commit
	case pbftpb.PBFTMsgType_MSG_VIEWCHANGE:
		viewChange := &pbftpb.ViewChange{}
		if err := proto.Unmarshal(pbftMsg.Msg, viewChange); err != nil {
			consensus.logger.Warnf("unmarshal ViewChange failed: %v", err)
			return nil
		}
		consensusMsg.Msg = viewChange
	case pbftpb.PBFTMsgType_MSG_NEWVIEW:
		newView := &pbftpb.NewView{}
		if err := proto.Unmarshal(pbftMsg.Msg, newView); err != nil {
			consensus.logger.Warnf("unmarshal NewView failed: %v", err)
			return nil
		}
		consensusMsg.Msg = newView
	default:
		consensus.logger.Warnf("unknown PBFT message type: %v", pbftMsg.Type)
		return nil
	}

	return consensusMsg
}

// extractConsensusConfig extracts consensus configuration
func (consensus *ConsensusPBFTImpl) extractConsensusConfig(config *config.ConsensusConfig) (validators []string,
	timeoutPrePrepare time.Duration, timeoutPrepare time.Duration, timeoutCommit time.Duration,
	timeoutViewChange time.Duration, err error) {
	timeoutPrePrepare = DefaultTimeoutPrePrepare
	timeoutPrepare = DefaultTimeoutPrepare
	timeoutCommit = DefaultTimeoutCommit
	timeoutViewChange = DefaultTimeoutViewChange

	validators, err = GetValidatorListFromConfig(consensus.chainConf.ChainConfig())
	if err != nil {
		consensus.logger.Errorf("[%s](%d/%d/%v) get validator list from config failed: %v",
			consensus.Id, consensus.Sequence, consensus.View, consensus.Step, err)
		return
	}

	for _, v := range config.ExtConfig {
		var parseErr error
		switch v.Key {
		case PBFT_timeout_preprepare_key:
			timeoutPrePrepare, parseErr = time.ParseDuration(v.Value)
			if parseErr != nil {
				consensus.logger.Warnf("[%s] parse timeout_preprepare failed: %v, using default",
					consensus.Id, parseErr)
			}
		case PBFT_timeout_prepare_key:
			timeoutPrepare, parseErr = time.ParseDuration(v.Value)
			if parseErr != nil {
				consensus.logger.Warnf("[%s] parse timeout_prepare failed: %v, using default",
					consensus.Id, parseErr)
			}
		case PBFT_timeout_commit_key:
			timeoutCommit, parseErr = time.ParseDuration(v.Value)
			if parseErr != nil {
				consensus.logger.Warnf("[%s] parse timeout_commit failed: %v, using default",
					consensus.Id, parseErr)
			}
		case PBFT_timeout_viewchange_key:
			timeoutViewChange, parseErr = time.ParseDuration(v.Value)
			if parseErr != nil {
				consensus.logger.Warnf("[%s] parse timeout_viewchange failed: %v, using default",
					consensus.Id, parseErr)
			}
		}
	}

	return
}

// handle is the main method of consensus process
func (consensus *ConsensusPBFTImpl) handle() {
	consensus.logger.Infof("[%s] handle start", consensus.Id)
	defer consensus.logger.Infof("[%s] handle end", consensus.Id)

	loop := true
	for loop {
		select {
		case proposedBlock := <-consensus.proposedBlockC:
			consensus.handleProposedBlock(proposedBlock)
		case result := <-consensus.verifyResultC:
			consensus.handleVerifyResult(result)
		case height := <-consensus.blockHeightC:
			consensus.handleBlockHeight(height)
		case msg := <-consensus.externalMsgC:
			consensus.logger.Debugf("[%s] receive from externalMsgC %s", consensus.Id, msg.Type)
			consensus.handleConsensusMsg(msg)
		case ti := <-consensus.timeScheduler.GetTimeoutC():
			consensus.handleTimeout(ti)
		case <-consensus.closeC:
			loop = false
		}
	}
}

// replayWal replays WAL entries
func (consensus *ConsensusPBFTImpl) replayWal() error {
	currentHeight, err := consensus.ledgerCache.CurrentHeight()
	if err != nil {
		return err
	}

	// If no write wal, enter new sequence directly
	if consensus.walWriteMode == wal_service.NonWalWrite {
		consensus.logger.Infof("[%s] no write wal, enter new sequence(%d) directly", consensus.Id, currentHeight+1)
		consensus.enterNewSequence(currentHeight + 1)
		return nil
	}

	// If write wal, need to replay wal
	lastEntry := &pbftpb.WalEntry{}
	it := consensus.wal.NewLogIterator()
	if it == nil {
		consensus.logger.Infof("[%s] no wal entries, enter new sequence(%d) directly", consensus.Id, currentHeight+1)
		consensus.enterNewSequence(currentHeight + 1)
		return nil
	}
	it.SkipToLast()
	defer it.Release()

	if it.HasPre() {
		lastData, err := it.Previous().Get()
		if err != nil {
			return err
		}
		mustUnmarshal(lastData, lastEntry)
	}

	sequence := lastEntry.Height
	consensus.logger.Infof("[%s] replayWal chainHeight: %d and walSequence: %d",
		consensus.Id, currentHeight, sequence)

	if currentHeight+1 < sequence {
		consensus.logger.Errorf("[%s] replay currentHeight: %v < sequence-1: %v, this should not happen",
			consensus.Id, currentHeight, sequence-1)
		return fmt.Errorf("wal sequence ahead of chain height")
	}

	if currentHeight >= sequence {
		// Consensus is slower than ledger
		consensus.enterNewSequence(currentHeight + 1)
		return nil
	}

	// Replay wal log, currentHeight=sequence-1
	consensus.enterNewSequence(sequence)

	switch lastEntry.Type {
	case pbftpb.WalEntryType_PREPREPARE_ENTRY:
		prePrepare := new(pbftpb.PrePrepare)
		mustUnmarshal(lastEntry.Data, prePrepare)
		err := consensus.enterPrepareFromReplayWal(prePrepare)
		if err != nil {
			return err
		}
	case pbftpb.WalEntryType_PREPARE_ENTRY:
		// Replay prepare votes
		prepareVoteSet := new(pbftpb.PrepareVoteSet)
		mustUnmarshal(lastEntry.Data, prepareVoteSet)
		consensus.PrepareVoteSet = prepareVoteSet
		consensus.Step = pbftpb.PBFTStep_PREPARE
		consensus.logger.Infof("[%s] replayed prepare vote set", consensus.Id)
	case pbftpb.WalEntryType_COMMIT_ENTRY:
		// Replay commit votes
		commitVoteSet := new(pbftpb.CommitVoteSet)
		mustUnmarshal(lastEntry.Data, commitVoteSet)
		consensus.CommitVoteSet = commitVoteSet
		consensus.Step = pbftpb.PBFTStep_COMMIT
		consensus.logger.Infof("[%s] replayed commit vote set", consensus.Id)
	default:
		consensus.logger.Warnf("[%s] wal replay found unrecognized type[%v], this should not happen",
			consensus.Id, lastEntry.Type)
	}
	return nil
}

// enterPrepareFromReplayWal enters prepare phase from WAL replay
func (consensus *ConsensusPBFTImpl) enterPrepareFromReplayWal(prePrepare *pbftpb.PrePrepare) error {
	if prePrepare == nil || prePrepare.Block == nil {
		consensus.logger.Warnf("enterPrepareFromReplayWal failed, pre-prepare is nil or has no block")
		return fmt.Errorf("pre-prepare is nil or has no block")
	}

	consensus.logger.Infof("[%s](%d/%d/%s) enter prepare from replay wal",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step)

	consensus.PrePrepare = prePrepare
	consensus.Step = pbftpb.PBFTStep_PRE_PREPARE

	// Enter prepare phase
	consensus.enterPrepare()

	consensus.logger.Infof("enterPrepareFromReplayWal succeed")
	return nil
}

// handleProposedBlock processes the proposed block
// This is called when the primary node receives a proposed block from core engine
func (consensus *ConsensusPBFTImpl) handleProposedBlock(proposedProposal *proposedProposal) {
	consensus.Lock()
	defer consensus.Unlock()

	block := proposedProposal.proposedBlock.Block
	consensus.logger.Infof("[%s](%d/%d/%s) receive block from core engine (%d/%x), isPrimary: %v",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step,
		block.Header.BlockHeight, block.Header.BlockHash, consensus.isPrimary())

	// Only process blocks for current sequence and if we are primary
	if block.Header.BlockHeight != consensus.Sequence {
		consensus.logger.Warnf("[%s](%d/%d/%s) receive block from invalid sequence: %d",
			consensus.Id, consensus.Sequence, consensus.View, consensus.Step, block.Header.BlockHeight)
		return
	}

	if !consensus.isPrimary() {
		consensus.logger.Warnf("[%s](%d/%d/%s) receive proposal but is not primary",
			consensus.Id, consensus.Sequence, consensus.View, consensus.Step)
		return
	}

	if consensus.Step != pbftpb.PBFTStep_NEW_HEIGHT && consensus.Step != pbftpb.PBFTStep_PRE_PREPARE {
		consensus.logger.Warnf("[%s](%d/%d/%s) receive proposal at wrong step",
			consensus.Id, consensus.Sequence, consensus.View, consensus.Step)
		return
	}

	// Add hash and signature to the block
	hash, sig, err := utils.SignBlock(consensus.chainConf.ChainConfig().Crypto.Hash, consensus.signer, block)
	if err != nil {
		consensus.logger.Errorf("[%s] sign block failed, %s", consensus.Id, err)
		return
	}
	block.Header.BlockHash = hash[:]
	block.Header.Signature = sig

	// Calculate digest (block hash)
	digest := block.Header.BlockHash

	// Create PrePrepare message
	prePrepare := &pbftpb.PrePrepare{
		Primary:  consensus.Id,
		View:     consensus.View,
		Sequence: consensus.Sequence,
		Digest:   digest,
		Block:    block,
		TxsRwSet: proposedProposal.proposedBlock.TxsRwSet,
	}

	// Sign PrePrepare message
	if err := consensus.signPrePrepare(prePrepare); err != nil {
		consensus.logger.Errorf("[%s] sign pre-prepare failed: %v", consensus.Id, err)
		return
	}

	consensus.PrePrepare = prePrepare
	consensus.Step = pbftpb.PBFTStep_PRE_PREPARE

	consensus.logger.Infof("[%s](%d/%d/%s) generated pre-prepare (%d/%d/%x)",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step,
		prePrepare.Sequence, prePrepare.View, prePrepare.Digest)

	// Save to WAL if enabled
	if consensus.walWriteMode != wal_service.NonWalWrite {
		prePrepareData := mustMarshal(prePrepare)
		consensus.saveWalEntry(pbftpb.WalEntryType_PREPREPARE_ENTRY, consensus.Sequence, prePrepareData)
	}

	// Set timeout for pre-prepare phase
	consensus.timeScheduler.AddTimeoutInfo(pbftpb.TimeoutInfo{
		Duration: consensus.TimeoutPrePrepare.Nanoseconds(),
		Height:   consensus.Sequence,
		Round:    consensus.View,
		Step:     pbftpb.PBFTStep_PRE_PREPARE,
	})

	// Broadcast PrePrepare message
	consensus.sendConsensusPrePrepare(prePrepare, "")

	// Tell core engine that we are no longer proposing
	consensus.sendProposeState(false)

	// Enter Prepare phase (primary also sends Prepare)
	consensus.enterPrepare()
}

// handleVerifyResult processes validation results
// This is called when a backup node verifies the block successfully
func (consensus *ConsensusPBFTImpl) handleVerifyResult(verifyResult *consensuspb.VerifyResult) {
	consensus.Lock()
	defer consensus.Unlock()

	height := verifyResult.VerifiedBlock.Header.BlockHeight
	hash := verifyResult.VerifiedBlock.Header.BlockHash
	consensus.logger.Infof("[%s](%d/%d/%s) receive verify result (%d/%x) %v",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step,
		height, hash, verifyResult.Code)

	if consensus.Sequence != height {
		consensus.logger.Warnf("[%s](%d/%d/%s) receive verify result for wrong sequence: %d",
			consensus.Id, consensus.Sequence, consensus.View, consensus.Step, height)
		return
	}

	if consensus.PrePrepare == nil {
		consensus.logger.Warnf("[%s](%d/%d/%s) receive verify result but PrePrepare is nil",
			consensus.Id, consensus.Sequence, consensus.View, consensus.Step)
		return
	}

	if !bytes.Equal(consensus.PrePrepare.Digest, hash) {
		consensus.logger.Warnf("[%s](%d/%d/%s) verify result hash mismatch: expected %x, got %x",
			consensus.Id, consensus.Sequence, consensus.View, consensus.Step,
			consensus.PrePrepare.Digest, hash)
		return
	}

	if verifyResult.Code == consensuspb.VerifyResult_FAIL {
		consensus.logger.Warnf("[%s](%d/%d/%s) block verification failed",
			consensus.Id, consensus.Sequence, consensus.View, consensus.Step)
		consensus.PrePrepare = nil
		// Trigger view change on verification failure
		consensus.enterViewChange()
		return
	}

	// Update block with verified block
	consensus.PrePrepare.Block = verifyResult.VerifiedBlock
	consensus.PrePrepare.TxsRwSet = verifyResult.TxsRwSet

	// Update digest to match verified block
	consensus.PrePrepare.Digest = verifyResult.VerifiedBlock.Header.BlockHash

	// Enter Prepare phase
	consensus.enterPrepare()
}

// handleBlockHeight processes block height messages
func (consensus *ConsensusPBFTImpl) handleBlockHeight(height uint64) {
	consensus.Lock()
	defer consensus.Unlock()

	consensus.logger.Infof("[%s](%d/%d/%s) receive block height %d",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step, height)

	// Check if we need to reset state due to block commit
	// If we are stuck in PREPARE or COMMIT phase and receive a block height that means
	// a block has been committed, we should reset to NEW_HEIGHT state
	if consensus.Sequence > height {
		// If we are stuck in PREPARE/COMMIT for a sequence that has already been committed,
		// we should reset to the correct sequence
		if consensus.Sequence == height+1 {
			// We are at height+1 (the next sequence), but the block at height has been committed
			// This means we should be at NEW_HEIGHT state for sequence height+1
			if consensus.Step == pbftpb.PBFTStep_PREPARE || consensus.Step == pbftpb.PBFTStep_COMMIT ||
				consensus.Step == pbftpb.PBFTStep_COMMITTED {
				consensus.logger.Infof("[%s](%d/%d/%s) resetting stuck state, block %d already committed",
					consensus.Id, consensus.Sequence, consensus.View, consensus.Step, height)
				// Reset to NEW_HEIGHT state without changing sequence
				consensus.Step = pbftpb.PBFTStep_NEW_HEIGHT
				consensus.PrePrepare = nil
				consensus.PrepareVoteSet = nil
				consensus.CommitVoteSet = nil
				consensus.sendProposeState(consensus.isPrimary())
			}
		}
		consensus.logger.Debugf("[%s](%d/%d/%s) receive outdated block height: %d",
			consensus.Id, consensus.Sequence, consensus.View, consensus.Step, height)
		return
	}

	// Enter new sequence
	consensus.enterNewSequence(height + 1)
}

// handleConsensusMsg handles consensus messages
func (consensus *ConsensusPBFTImpl) handleConsensusMsg(msg *ConsensusMsg) {
	consensus.Lock()
	defer consensus.Unlock()

	switch msg.Type {
	case pbftpb.PBFTMsgType_MSG_PREPREPARE:
		consensus.procPrePrepare(msg.Msg.(*pbftpb.PrePrepare))
	case pbftpb.PBFTMsgType_MSG_PREPARE:
		consensus.procPrepare(msg.Msg.(*pbftpb.Prepare))
	case pbftpb.PBFTMsgType_MSG_COMMIT:
		consensus.procCommit(msg.Msg.(*pbftpb.Commit))
	case pbftpb.PBFTMsgType_MSG_VIEWCHANGE:
		consensus.procViewChange(msg.Msg.(*pbftpb.ViewChange))
	case pbftpb.PBFTMsgType_MSG_NEWVIEW:
		consensus.procNewView(msg.Msg.(*pbftpb.NewView))
	}
}

// handleTimeout handles timeout events
func (consensus *ConsensusPBFTImpl) handleTimeout(ti pbftpb.TimeoutInfo) {
	consensus.Lock()
	defer consensus.Unlock()

	consensus.logger.Infof("[%s](%d/%d/%s) handleTimeout ti: %v",
		consensus.Id, consensus.Sequence, consensus.View, consensus.Step, ti)

	// Check if timeout is for current sequence
	if ti.Height != consensus.Sequence {
		consensus.logger.Debugf("[%s] ignore outdated timeout: %v", consensus.Id, ti)
		return
	}

	switch ti.Step {
	case pbftpb.PBFTStep_PRE_PREPARE, pbftpb.PBFTStep_PREPARE:
		// Timeout in PrePrepare or Prepare phase
		// Before triggering view change, check if we can reset state from ledger
		currentHeight, err := consensus.ledgerCache.CurrentHeight()
		if err == nil && consensus.Sequence == currentHeight+1 {
			// We are stuck at the next expected height, check if we should reset
			consensus.logger.Infof("[%s](%d/%d/%s) timeout in PREPARE, checking ledger height %d",
				consensus.Id, consensus.Sequence, consensus.View, consensus.Step, currentHeight)
			// The block at currentHeight has been committed, we should be at NEW_HEIGHT
			consensus.Step = pbftpb.PBFTStep_NEW_HEIGHT
			consensus.PrePrepare = nil
			consensus.PrepareVoteSet = nil
			consensus.CommitVoteSet = nil
			consensus.sendProposeState(consensus.isPrimary())
			consensus.logger.Infof("[%s] reset to NEW_HEIGHT state after timeout", consensus.Id)
			return
		}
		// Otherwise, trigger view change
		consensus.enterViewChange()
	case pbftpb.PBFTStep_COMMIT:
		// Timeout in Commit phase, similar check
		currentHeight, err := consensus.ledgerCache.CurrentHeight()
		if err == nil && consensus.Sequence == currentHeight+1 {
			consensus.logger.Infof("[%s](%d/%d/%s) timeout in COMMIT, checking ledger height %d",
				consensus.Id, consensus.Sequence, consensus.View, consensus.Step, currentHeight)
			consensus.Step = pbftpb.PBFTStep_NEW_HEIGHT
			consensus.PrePrepare = nil
			consensus.PrepareVoteSet = nil
			consensus.CommitVoteSet = nil
			consensus.sendProposeState(consensus.isPrimary())
			consensus.logger.Infof("[%s] reset to NEW_HEIGHT state after timeout", consensus.Id)
			return
		}
		// Otherwise, trigger view change
		consensus.enterViewChange()
	case pbftpb.PBFTStep_VIEW_CHANGE:
		// View change timeout, try next view
		consensus.enterViewChange()
	}
}
