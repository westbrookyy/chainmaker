/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.

SPDX-License-Identifier: Apache-2.0
*/

package pbft

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewValidatorSet 测试创建验证者集合
func TestNewValidatorSet(t *testing.T) {
	validators := []string{"node3", "node1", "node2", "node4"}
	vs := newValidatorSet(newTestLogger(), validators)

	require.NotNil(t, vs)
	require.Equal(t, 4, len(vs.Validators))
	// 验证排序
	require.Equal(t, "node1", vs.Validators[0])
	require.Equal(t, "node2", vs.Validators[1])
	require.Equal(t, "node3", vs.Validators[2])
	require.Equal(t, "node4", vs.Validators[3])
}

// TestValidatorSet_Size 测试验证者集合大小
func TestValidatorSet_Size(t *testing.T) {
	validators := []string{"node1", "node2", "node3"}
	vs := newValidatorSet(newTestLogger(), validators)

	require.Equal(t, int32(3), vs.Size())

	// 空集合
	emptyVs := newValidatorSet(newTestLogger(), []string{})
	require.Equal(t, int32(0), emptyVs.Size())

	// nil集合
	var nilVs *validatorSet
	require.Equal(t, int32(0), nilVs.Size())
}

// TestValidatorSet_HasValidator 测试验证者是否存在
func TestValidatorSet_HasValidator(t *testing.T) {
	validators := []string{"node1", "node2", "node3"}
	vs := newValidatorSet(newTestLogger(), validators)

	require.True(t, vs.HasValidator("node1"))
	require.True(t, vs.HasValidator("node2"))
	require.True(t, vs.HasValidator("node3"))
	require.False(t, vs.HasValidator("node4"))
	require.False(t, vs.HasValidator(""))

	// nil集合
	var nilVs *validatorSet
	require.False(t, nilVs.HasValidator("node1"))
}

// TestValidatorSet_GetPrimary 测试获取主节点
func TestValidatorSet_GetPrimary(t *testing.T) {
	validators := []string{"node1", "node2", "node3", "node4"}
	vs := newValidatorSet(newTestLogger(), validators)

	// view=0, blockNumber=1: (0+1) % 4 = 1 -> node2
	primary, err := vs.GetPrimary(0, 1)
	require.Nil(t, err)
	require.Equal(t, "node2", primary)

	// view=0, blockNumber=0: (0+0) % 4 = 0 -> node1
	primary, err = vs.GetPrimary(0, 0)
	require.Nil(t, err)
	require.Equal(t, "node1", primary)

	// view=1, blockNumber=1: (1+1) % 4 = 2 -> node3
	primary, err = vs.GetPrimary(1, 1)
	require.Nil(t, err)
	require.Equal(t, "node3", primary)

	// view=2, blockNumber=5: (2+5) % 4 = 3 -> node4
	primary, err = vs.GetPrimary(2, 5)
	require.Nil(t, err)
	require.Equal(t, "node4", primary)
}

// TestValidatorSet_GetPrimary_Empty 测试空集合获取主节点
func TestValidatorSet_GetPrimary_Empty(t *testing.T) {
	emptyVs := newValidatorSet(newTestLogger(), []string{})
	_, err := emptyVs.GetPrimary(0, 1)
	require.NotNil(t, err)
	require.Equal(t, ErrInvalidIndex, err)

	var nilVs *validatorSet
	_, err = nilVs.GetPrimary(0, 1)
	require.NotNil(t, err)
	require.Equal(t, ErrInvalidIndex, err)
}

// TestValidatorSet_UpdateValidators 测试更新验证者集合
func TestValidatorSet_UpdateValidators(t *testing.T) {
	validators := []string{"node1", "node2", "node3"}
	vs := newValidatorSet(newTestLogger(), validators)

	// 添加新节点
	newValidators := []string{"node1", "node2", "node3", "node4"}
	added, removed, err := vs.updateValidators(newValidators)
	require.Nil(t, err)
	require.Equal(t, 1, len(added))
	require.Equal(t, "node4", added[0])
	require.Equal(t, 0, len(removed))
	require.Equal(t, 4, len(vs.Validators))

	// 移除节点
	newValidators = []string{"node1", "node2"}
	added, removed, err = vs.updateValidators(newValidators)
	require.Nil(t, err)
	require.Equal(t, 0, len(added))
	require.Equal(t, 2, len(removed))
	require.Contains(t, removed, "node3")
	require.Contains(t, removed, "node4")
	require.Equal(t, 2, len(vs.Validators))

	// 同时添加和移除
	newValidators = []string{"node1", "node5", "node6"}
	added, removed, err = vs.updateValidators(newValidators)
	require.Nil(t, err)
	require.Equal(t, 2, len(added))
	require.Contains(t, added, "node5")
	require.Contains(t, added, "node6")
	require.Equal(t, 1, len(removed))
	require.Equal(t, "node2", removed[0])
	require.Equal(t, 3, len(vs.Validators))
}

// TestValidatorSet_UpdateValidators_NoChange 测试无变化的更新
func TestValidatorSet_UpdateValidators_NoChange(t *testing.T) {
	validators := []string{"node1", "node2", "node3"}
	vs := newValidatorSet(newTestLogger(), validators)

	// 相同节点，不同顺序
	newValidators := []string{"node3", "node1", "node2"}
	added, removed, err := vs.updateValidators(newValidators)
	require.Nil(t, err)
	require.Equal(t, 0, len(added))
	require.Equal(t, 0, len(removed))
	require.Equal(t, 3, len(vs.Validators))
}

// TestValidatorSet_GetByIndex 测试通过索引获取验证者
func TestValidatorSet_GetByIndex(t *testing.T) {
	validators := []string{"node1", "node2", "node3", "node4"}
	vs := newValidatorSet(newTestLogger(), validators)

	// 有效索引
	val, err := vs.getByIndex(0)
	require.Nil(t, err)
	require.Equal(t, "node1", val)

	val, err = vs.getByIndex(2)
	require.Nil(t, err)
	require.Equal(t, "node3", val)

	// 无效索引
	_, err = vs.getByIndex(-1)
	require.NotNil(t, err)
	require.Equal(t, ErrInvalidIndex, err)

	_, err = vs.getByIndex(4)
	require.NotNil(t, err)
	require.Equal(t, ErrInvalidIndex, err)
}

// TestValidatorSet_GetIndexByString 测试通过字符串获取索引
func TestValidatorSet_GetIndexByString(t *testing.T) {
	validators := []string{"node1", "node2", "node3"}
	vs := newValidatorSet(newTestLogger(), validators)

	require.Equal(t, int32(0), vs.getIndexByString("node1"))
	require.Equal(t, int32(1), vs.getIndexByString("node2"))
	require.Equal(t, int32(2), vs.getIndexByString("node3"))
	require.Equal(t, int32(-1), vs.getIndexByString("node4"))
	require.Equal(t, int32(-1), vs.getIndexByString(""))
}

// TestValidatorSet_IsNilOrEmpty 测试集合是否为空
func TestValidatorSet_IsNilOrEmpty(t *testing.T) {
	// nil集合
	var nilVs *validatorSet
	require.True(t, nilVs.isNilOrEmpty())

	// 空集合
	emptyVs := newValidatorSet(newTestLogger(), []string{})
	require.True(t, emptyVs.isNilOrEmpty())

	// 非空集合
	validators := []string{"node1"}
	vs := newValidatorSet(newTestLogger(), validators)
	require.False(t, vs.isNilOrEmpty())
}

// TestValidatorSet_String 测试字符串转换
func TestValidatorSet_String(t *testing.T) {
	validators := []string{"node1", "node2", "node3"}
	vs := newValidatorSet(newTestLogger(), validators)

	str := vs.String()
	require.NotEmpty(t, str)
	require.Contains(t, str, "node1")

	// nil集合
	var nilVs *validatorSet
	require.Empty(t, nilVs.String())
}
