/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.

SPDX-License-Identifier: Apache-2.0
*/

package pbft

import (
	"testing"

	pbftpb "chainmaker.org/chainmaker/pb-go/v2/consensus/pbft"
	"github.com/stretchr/testify/require"
)

// TestNewPrepareVoteSet 测试创建Prepare投票集合
func TestNewPrepareVoteSet(t *testing.T) {
	validators := []string{"node1", "node2", "node3", "node4"}
	vs := newValidatorSet(newTestLogger(), validators)

	voteSet := NewPrepareVoteSet(newTestLogger(), 1, 1, []byte("digest"), vs)
	require.NotNil(t, voteSet)
	require.Equal(t, uint64(1), voteSet.View)
	require.Equal(t, uint64(1), voteSet.Sequence)
	require.Equal(t, []byte("digest"), voteSet.Digest)
	require.NotNil(t, voteSet.Votes)
	require.Equal(t, uint64(0), voteSet.Sum)
}

// TestPrepareVoteSet_AddVote 测试添加Prepare投票
func TestPrepareVoteSet_AddVote(t *testing.T) {
	validators := []string{"node1", "node2", "node3", "node4"}
	vs := newValidatorSet(newTestLogger(), validators)

	voteSet := NewPrepareVoteSet(newTestLogger(), 1, 1, []byte("digest"), vs)

	// 添加有效投票
	prepare := &pbftpb.Prepare{
		NodeId:   "node1",
		View:     1,
		Sequence: 1,
		Digest:   []byte("digest"),
	}
	added, err := voteSet.AddVote(prepare)
	require.Nil(t, err)
	require.True(t, added)
	require.Equal(t, uint64(1), voteSet.Sum)
	require.Equal(t, prepare, voteSet.Votes["node1"])

	// 添加重复投票
	added, err = voteSet.AddVote(prepare)
	require.Nil(t, err)
	require.False(t, added) // 已存在，不添加
	require.Equal(t, uint64(1), voteSet.Sum)

	// 添加另一个节点的投票
	prepare2 := &pbftpb.Prepare{
		NodeId:   "node2",
		View:     1,
		Sequence: 1,
		Digest:   []byte("digest"),
	}
	added, err = voteSet.AddVote(prepare2)
	require.Nil(t, err)
	require.True(t, added)
	require.Equal(t, uint64(2), voteSet.Sum)
}

// TestPrepareVoteSet_AddVote_Invalid 测试添加无效投票
func TestPrepareVoteSet_AddVote_Invalid(t *testing.T) {
	validators := []string{"node1", "node2", "node3", "node4"}
	vs := newValidatorSet(newTestLogger(), validators)

	voteSet := NewPrepareVoteSet(newTestLogger(), 1, 1, []byte("digest"), vs)

	// nil投票
	added, err := voteSet.AddVote(nil)
	require.NotNil(t, err)
	require.False(t, added)
	require.Equal(t, ErrVoteNil, err)

	// View不匹配
	prepare := &pbftpb.Prepare{
		NodeId:   "node1",
		View:     2, // 错误
		Sequence: 1,
		Digest:   []byte("digest"),
	}
	added, err = voteSet.AddVote(prepare)
	require.NotNil(t, err)
	require.False(t, added)

	// Sequence不匹配
	prepare = &pbftpb.Prepare{
		NodeId:   "node1",
		View:     1,
		Sequence: 2, // 错误
		Digest:   []byte("digest"),
	}
	added, err = voteSet.AddVote(prepare)
	require.NotNil(t, err)
	require.False(t, added)

	// Digest不匹配
	prepare = &pbftpb.Prepare{
		NodeId:   "node1",
		View:     1,
		Sequence: 1,
		Digest:   []byte("wrong"), // 错误
	}
	added, err = voteSet.AddVote(prepare)
	require.NotNil(t, err)
	require.False(t, added)

	// 无效验证者
	prepare = &pbftpb.Prepare{
		NodeId:   "node5", // 不在验证者列表中
		View:     1,
		Sequence: 1,
		Digest:   []byte("digest"),
	}
	added, err = voteSet.AddVote(prepare)
	require.NotNil(t, err)
	require.False(t, added)
}

// TestPrepareVoteSet_HasTwoThirdsMajority 测试2/3多数判断
func TestPrepareVoteSet_HasTwoThirdsMajority(t *testing.T) {
	// 4节点：需要3个投票（2f+1 = 2*1+1 = 3）
	validators := []string{"node1", "node2", "node3", "node4"}
	vs := newValidatorSet(newTestLogger(), validators)

	voteSet := NewPrepareVoteSet(newTestLogger(), 1, 1, []byte("digest"), vs)

	// 0个投票
	require.False(t, voteSet.HasTwoThirdsMajority())

	// 1个投票
	voteSet.AddVote(&pbftpb.Prepare{NodeId: "node1", View: 1, Sequence: 1, Digest: []byte("digest")})
	require.False(t, voteSet.HasTwoThirdsMajority())

	// 2个投票
	voteSet.AddVote(&pbftpb.Prepare{NodeId: "node2", View: 1, Sequence: 1, Digest: []byte("digest")})
	require.False(t, voteSet.HasTwoThirdsMajority())

	// 3个投票（达到2/3多数）
	voteSet.AddVote(&pbftpb.Prepare{NodeId: "node3", View: 1, Sequence: 1, Digest: []byte("digest")})
	require.True(t, voteSet.HasTwoThirdsMajority())

	// 7节点：需要5个投票（2f+1 = 2*2+1 = 5）
	validators7 := []string{"node1", "node2", "node3", "node4", "node5", "node6", "node7"}
	vs7 := newValidatorSet(newTestLogger(), validators7)
	voteSet7 := NewPrepareVoteSet(newTestLogger(), 1, 1, []byte("digest"), vs7)

	// 4个投票
	for i := 1; i <= 4; i++ {
		voteSet7.AddVote(&pbftpb.Prepare{
			NodeId:   validators7[i-1],
			View:     1,
			Sequence: 1,
			Digest:   []byte("digest"),
		})
	}
	require.False(t, voteSet7.HasTwoThirdsMajority())

	// 5个投票（达到2/3多数）
	voteSet7.AddVote(&pbftpb.Prepare{
		NodeId:   validators7[4],
		View:     1,
		Sequence: 1,
		Digest:   []byte("digest"),
	})
	require.True(t, voteSet7.HasTwoThirdsMajority())
}

// TestNewCommitVoteSet 测试创建Commit投票集合
func TestNewCommitVoteSet(t *testing.T) {
	validators := []string{"node1", "node2", "node3", "node4"}
	vs := newValidatorSet(newTestLogger(), validators)

	voteSet := NewCommitVoteSet(newTestLogger(), 1, 1, []byte("digest"), vs)
	require.NotNil(t, voteSet)
	require.Equal(t, uint64(1), voteSet.View)
	require.Equal(t, uint64(1), voteSet.Sequence)
	require.Equal(t, []byte("digest"), voteSet.Digest)
	require.NotNil(t, voteSet.Votes)
	require.Equal(t, uint64(0), voteSet.Sum)
}

// TestCommitVoteSet_AddVote 测试添加Commit投票
func TestCommitVoteSet_AddVote(t *testing.T) {
	validators := []string{"node1", "node2", "node3", "node4"}
	vs := newValidatorSet(newTestLogger(), validators)

	voteSet := NewCommitVoteSet(newTestLogger(), 1, 1, []byte("digest"), vs)

	// 添加有效投票
	commit := &pbftpb.Commit{
		NodeId:   "node1",
		View:     1,
		Sequence: 1,
		Digest:   []byte("digest"),
	}
	added, err := voteSet.AddVote(commit)
	require.Nil(t, err)
	require.True(t, added)
	require.Equal(t, uint64(1), voteSet.Sum)
	require.Equal(t, commit, voteSet.Votes["node1"])

	// 添加重复投票
	added, err = voteSet.AddVote(commit)
	require.Nil(t, err)
	require.False(t, added)
	require.Equal(t, uint64(1), voteSet.Sum)
}

// TestCommitVoteSet_AddVote_Invalid 测试添加无效Commit投票
func TestCommitVoteSet_AddVote_Invalid(t *testing.T) {
	validators := []string{"node1", "node2", "node3", "node4"}
	vs := newValidatorSet(newTestLogger(), validators)

	voteSet := NewCommitVoteSet(newTestLogger(), 1, 1, []byte("digest"), vs)

	// nil投票
	added, err := voteSet.AddVote(nil)
	require.NotNil(t, err)
	require.False(t, added)
	require.Equal(t, ErrVoteNil, err)

	// View不匹配
	commit := &pbftpb.Commit{
		NodeId:   "node1",
		View:     2, // 错误
		Sequence: 1,
		Digest:   []byte("digest"),
	}
	added, err = voteSet.AddVote(commit)
	require.NotNil(t, err)
	require.False(t, added)

	// Digest不匹配
	commit = &pbftpb.Commit{
		NodeId:   "node1",
		View:     1,
		Sequence: 1,
		Digest:   []byte("wrong"), // 错误
	}
	added, err = voteSet.AddVote(commit)
	require.NotNil(t, err)
	require.False(t, added)
}

// TestCommitVoteSet_HasTwoThirdsMajority 测试Commit投票2/3多数判断
func TestCommitVoteSet_HasTwoThirdsMajority(t *testing.T) {
	// 4节点：需要3个投票
	validators := []string{"node1", "node2", "node3", "node4"}
	vs := newValidatorSet(newTestLogger(), validators)

	voteSet := NewCommitVoteSet(newTestLogger(), 1, 1, []byte("digest"), vs)

	// 0个投票
	require.False(t, voteSet.HasTwoThirdsMajority())

	// 2个投票
	voteSet.AddVote(&pbftpb.Commit{NodeId: "node1", View: 1, Sequence: 1, Digest: []byte("digest")})
	voteSet.AddVote(&pbftpb.Commit{NodeId: "node2", View: 1, Sequence: 1, Digest: []byte("digest")})
	require.False(t, voteSet.HasTwoThirdsMajority())

	// 3个投票（达到2/3多数）
	voteSet.AddVote(&pbftpb.Commit{NodeId: "node3", View: 1, Sequence: 1, Digest: []byte("digest")})
	require.True(t, voteSet.HasTwoThirdsMajority())
}

// TestVoteSet_Nil 测试nil投票集合
func TestVoteSet_Nil(t *testing.T) {
	var nilPrepareVoteSet *PrepareVoteSet
	require.False(t, nilPrepareVoteSet.HasTwoThirdsMajority())

	var nilCommitVoteSet *CommitVoteSet
	require.False(t, nilCommitVoteSet.HasTwoThirdsMajority())
}
