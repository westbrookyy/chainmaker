/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.

SPDX-License-Identifier: Apache-2.0
*/

package pbft

import (
	"sync"

	pbftpb "chainmaker.org/chainmaker/pb-go/v2/consensus/pbft"
	"chainmaker.org/chainmaker/protocol/v2"
	"github.com/gogo/protobuf/proto"
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
		Sequence:        cs.Sequence,
		Step:            cs.Step,
		PrePrepare:      cs.PrePrepare,
		PrepareVoteSet:  cs.PrepareVoteSet,
		CommitVoteSet:   cs.CommitVoteSet,
		ViewChangeVotes: cs.ViewChangeVotes,
		NewViewVotes:    cs.NewViewVotes,
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
// Note: This function creates a deep copy of the state to avoid sharing references
func (cache *consensusStateCache) addConsensusState(state *ConsensusState) {
	if state == nil || state.Sequence <= 0 {
		return
	}

	cache.Lock()
	defer cache.Unlock()

	// Create a deep copy of the state to avoid sharing references
	// When the original state is reset later, the cached state should remain unchanged
	cachedState := &ConsensusState{
		logger:          state.logger,
		Id:              state.Id,
		View:            state.View,
		Sequence:        state.Sequence,
		Step:            state.Step,
		PrePrepare:      nil,
		PrepareVoteSet:  nil,
		CommitVoteSet:   nil,
		ViewChangeVotes: make(map[string]*pbftpb.ViewChange),
		NewViewVotes:    make(map[string]*pbftpb.NewView),
	}

	// Deep copy PrePrepare if exists
	if state.PrePrepare != nil {
		if cloned, ok := proto.Clone(state.PrePrepare).(*pbftpb.PrePrepare); ok {
			cachedState.PrePrepare = cloned
		}
	}

	// Deep copy PrepareVoteSet if exists
	if state.PrepareVoteSet != nil {
		if cloned, ok := proto.Clone(state.PrepareVoteSet).(*pbftpb.PrepareVoteSet); ok {
			cachedState.PrepareVoteSet = cloned
		}
	}

	// Deep copy CommitVoteSet if exists
	if state.CommitVoteSet != nil {
		if cloned, ok := proto.Clone(state.CommitVoteSet).(*pbftpb.CommitVoteSet); ok {
			cachedState.CommitVoteSet = cloned
		}
	}

	// Deep copy ViewChangeVotes map
	if state.ViewChangeVotes != nil {
		for k, v := range state.ViewChangeVotes {
			if cloned, ok := proto.Clone(v).(*pbftpb.ViewChange); ok {
				cachedState.ViewChangeVotes[k] = cloned
			}
		}
	}

	// Deep copy NewViewVotes map
	if state.NewViewVotes != nil {
		for k, v := range state.NewViewVotes {
			if cloned, ok := proto.Clone(v).(*pbftpb.NewView); ok {
				cachedState.NewViewVotes[k] = cloned
			}
		}
	}

	cache.cache[state.Sequence] = cachedState
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

// gc deletes old cache entries, keeping only the most recent 'size' entries
// It removes entries where (k + cache.size) <= sequence, meaning only entries
// within the last 'size' sequences are kept
func (cache *consensusStateCache) gc(sequence uint64) {
	for k := range cache.cache {
		if (k + cache.size) <= sequence {
			delete(cache.cache, k)
		}
	}
}
