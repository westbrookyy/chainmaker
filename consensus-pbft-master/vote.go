/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.

SPDX-License-Identifier: Apache-2.0
*/

package pbft

import (
	"bytes"
	"errors"
	"fmt"

	pbftpb "chainmaker.org/chainmaker/pb-go/v2/consensus/pbft"
	"chainmaker.org/chainmaker/protocol/v2"
)

var (
	ErrVoteNil = errors.New("nil vote")
)

// PrepareVoteSet represents a set of prepare votes
type PrepareVoteSet struct {
	logger   protocol.Logger
	View     uint64
	Sequence uint64
	Digest   []byte
	Votes    map[string]*pbftpb.Prepare
	Sum      uint64
	validators *validatorSet
}

// NewPrepareVoteSet creates a new PrepareVoteSet instance
func NewPrepareVoteSet(logger protocol.Logger, view, sequence uint64, digest []byte,
	validators *validatorSet) *PrepareVoteSet {
	return &PrepareVoteSet{
		logger:    logger,
		View:      view,
		Sequence:  sequence,
		Digest:    digest,
		Votes:     make(map[string]*pbftpb.Prepare),
		validators: validators,
	}
}

// AddVote adds a prepare vote to the set
func (pvs *PrepareVoteSet) AddVote(prepare *pbftpb.Prepare) (added bool, err error) {
	if prepare == nil {
		return false, ErrVoteNil
	}

	if prepare.View != pvs.View || prepare.Sequence != pvs.Sequence {
		return false, fmt.Errorf("vote view/sequence mismatch: expected %d/%d, got %d/%d",
			pvs.View, pvs.Sequence, prepare.View, prepare.Sequence)
	}

	if !bytes.Equal(prepare.Digest, pvs.Digest) {
		return false, fmt.Errorf("vote digest mismatch")
	}

	if !pvs.validators.HasValidator(prepare.NodeId) {
		return false, fmt.Errorf("invalid validator: %s", prepare.NodeId)
	}

	if _, ok := pvs.Votes[prepare.NodeId]; ok {
		return false, nil // already have this vote
	}

	pvs.Votes[prepare.NodeId] = prepare
	pvs.Sum++
	return true, nil
}

// HasTwoThirdsMajority checks if the vote set has 2/3 majority
func (pvs *PrepareVoteSet) HasTwoThirdsMajority() bool {
	if pvs == nil {
		return false
	}
	quorum := uint64(pvs.validators.Size()*2/3 + 1)
	return pvs.Sum >= quorum
}

// CommitVoteSet represents a set of commit votes
type CommitVoteSet struct {
	logger   protocol.Logger
	View     uint64
	Sequence uint64
	Digest   []byte
	Votes    map[string]*pbftpb.Commit
	Sum      uint64
	validators *validatorSet
}

// NewCommitVoteSet creates a new CommitVoteSet instance
func NewCommitVoteSet(logger protocol.Logger, view, sequence uint64, digest []byte,
	validators *validatorSet) *CommitVoteSet {
	return &CommitVoteSet{
		logger:    logger,
		View:      view,
		Sequence:  sequence,
		Digest:    digest,
		Votes:     make(map[string]*pbftpb.Commit),
		validators: validators,
	}
}

// AddVote adds a commit vote to the set
func (cvs *CommitVoteSet) AddVote(commit *pbftpb.Commit) (added bool, err error) {
	if commit == nil {
		return false, ErrVoteNil
	}

	if commit.View != cvs.View || commit.Sequence != cvs.Sequence {
		return false, fmt.Errorf("vote view/sequence mismatch: expected %d/%d, got %d/%d",
			cvs.View, cvs.Sequence, commit.View, commit.Sequence)
	}

	if !bytes.Equal(commit.Digest, cvs.Digest) {
		return false, fmt.Errorf("vote digest mismatch")
	}

	if !cvs.validators.HasValidator(commit.NodeId) {
		return false, fmt.Errorf("invalid validator: %s", commit.NodeId)
	}

	if _, ok := cvs.Votes[commit.NodeId]; ok {
		return false, nil // already have this vote
	}

	cvs.Votes[commit.NodeId] = commit
	cvs.Sum++
	return true, nil
}

// HasTwoThirdsMajority checks if the vote set has 2/3 majority
func (cvs *CommitVoteSet) HasTwoThirdsMajority() bool {
	if cvs == nil {
		return false
	}
	quorum := uint64(cvs.validators.Size()*2/3 + 1)
	return cvs.Sum >= quorum
}
