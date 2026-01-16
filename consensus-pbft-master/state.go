/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.

SPDX-License-Identifier: Apache-2.0
*/

package pbft

import (
	"sync"

	pbftpb "chainmaker.org/chainmaker/pb-go/v2/consensus/pbft"
	"chainmaker.org/chainmaker/protocol/v2"
)

// ConsensusState represents the consensus state of the node in PBFT
type ConsensusState struct {
	logger protocol.Logger
	// node id
	Id string
	// current view
	View uint64
	// current sequence (block height)
	Sequence uint64
	// current step
	Step pbftpb.PBFTStep

	// pre-prepare message
	PrePrepare *pbftpb.PrePrepare
	// prepare vote set
	PrepareVoteSet *pbftpb.PrepareVoteSet
	// commit vote set
	CommitVoteSet *pbftpb.CommitVoteSet
	// view change votes
	ViewChangeVotes map[string]*pbftpb.ViewChange
	// new view votes
	NewViewVotes map[string]*pbftpb.NewView
}

// NewConsensusState creates a new ConsensusState instance
func NewConsensusState(logger protocol.Logger, id string) *ConsensusState {
	cs := &ConsensusState{
		logger:          logger,
		Id:              id,
		ViewChangeVotes: make(map[string]*pbftpb.ViewChange),
		NewViewVotes:    make(map[string]*pbftpb.NewView),
	}
	return cs
}

// toProto serializes the ConsensusState instance
func (cs *ConsensusState) toProto() *pbftpb.ConsensusState {
	if cs == nil {
		return nil
	}
	csProto := &pbftpb.ConsensusState{
		Id:              cs.Id,
		View:            cs.View,
		Sequence:       cs.Sequence,
		Step:            cs.Step,
		PrePrepare:     cs.PrePrepare,
		PrepareVoteSet: cs.PrepareVoteSet,
		CommitVoteSet:  cs.CommitVoteSet,
		ViewChangeVotes: cs.ViewChangeVotes,
		NewViewVotes:   cs.NewViewVotes,
	}
	return csProto
}

// consensusStateCache caches historical consensus state
type consensusStateCache struct {
	sync.Mutex
	size  uint64
	cache map[uint64]*ConsensusState
}

// newConsensusStateCache creates a new state cache
func newConsensusStateCache(size uint64) *consensusStateCache {
	return &consensusStateCache{
		size:  size,
		cache: make(map[uint64]*ConsensusState, size),
	}
}

// addConsensusState adds a new state to the cache
func (cache *consensusStateCache) addConsensusState(state *ConsensusState) {
	if state == nil || state.Sequence <= 0 {
		return
	}

	cache.Lock()
	defer cache.Unlock()

	cache.cache[state.Sequence] = state
	cache.gc(state.Sequence)
}

// getConsensusState gets the desired consensus state from the cache
func (cache *consensusStateCache) getConsensusState(sequence uint64) *ConsensusState {
	cache.Lock()
	defer cache.Unlock()

	if state, ok := cache.cache[sequence]; ok {
		return state
	}

	return nil
}

// gc deletes too many caches
func (cache *consensusStateCache) gc(sequence uint64) {
	for k := range cache.cache {
		if (k + cache.size) <= sequence {
			delete(cache.cache, k)
		}
	}
}
