/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.

SPDX-License-Identifier: Apache-2.0
*/

package pbft

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"chainmaker.org/chainmaker/protocol/v2"
)

var (
	// ErrInvalidIndex implements the error for invalid index of validators
	ErrInvalidIndex = errors.New("invalid index")
)

// validatorSet represents the set of validators in PBFT
type validatorSet struct {
	sync.Mutex
	logger     protocol.Logger
	Validators []string
	// Validator's current block height (reserved for future use, e.g., node status tracking)
	validatorsHeight map[string]uint64
	// Validator's beat Time (reserved for future use, e.g., node liveness detection)
	validatorsBeatTime map[string]int64
}

// newValidatorSet creates a new validator set
func newValidatorSet(logger protocol.Logger, validators []string) *validatorSet {
	sort.SliceStable(validators, func(i, j int) bool {
		return validators[i] < validators[j]
	})

	valSet := &validatorSet{
		logger:             logger,
		Validators:         validators,
		validatorsHeight:   make(map[string]uint64),
		validatorsBeatTime: make(map[string]int64),
	}
	valSet.logger.Infof("new validator set: %v", validators)

	return valSet
}

// isNilOrEmpty returns true when the validatorSet is nil or empty
func (valSet *validatorSet) isNilOrEmpty() bool {
	if valSet == nil {
		return true
	}
	valSet.Lock()
	defer valSet.Unlock()
	return len(valSet.Validators) == 0
}

// String converts *validatorSet to string
func (valSet *validatorSet) String() string {
	if valSet == nil {
		return ""
	}
	valSet.Lock()
	defer valSet.Unlock()

	return fmt.Sprintf("%v", valSet.Validators)
}

// Size returns validatorSet size
func (valSet *validatorSet) Size() int32 {
	if valSet == nil {
		return 0
	}

	valSet.Lock()
	defer valSet.Unlock()

	return int32(len(valSet.Validators))
}

// HasValidator returns whether validator is in the validatorSet
func (valSet *validatorSet) HasValidator(validator string) bool {
	if valSet == nil {
		return false
	}

	valSet.Lock()
	defer valSet.Unlock()

	return valSet.hasValidator(validator)
}

func (valSet *validatorSet) hasValidator(validator string) bool {
	for _, val := range valSet.Validators {
		if val == validator {
			return true
		}
	}
	return false
}

// GetPrimary calculates the primary node based on view and block number
// In PBFT, primary is determined by: primary = validators[(view + blocknumber) % len(validators)]
func (valSet *validatorSet) GetPrimary(view uint64, blockNumber uint64) (validator string, err error) {
	if valSet.isNilOrEmpty() {
		valSet.logger.Warnf("GetPrimary: validatorSet is nil or empty")
		return "", ErrInvalidIndex
	}

	valSet.Lock()
	defer valSet.Unlock()

	if len(valSet.Validators) == 0 {
		valSet.logger.Warnf("GetPrimary: validators list is empty")
		return "", ErrInvalidIndex
	}

	index := int((view + blockNumber) % uint64(len(valSet.Validators)))
	if index < 0 || index >= len(valSet.Validators) {
		valSet.logger.Errorf("GetPrimary: invalid index %d for validators list of size %d (view=%d, blockNumber=%d)", 
			index, len(valSet.Validators), view, blockNumber)
		return "", ErrInvalidIndex
	}
	
	primary := valSet.Validators[index]
	valSet.logger.Infof("GetPrimary: view=%d, blockNumber=%d, index=%d, primary=%s, validators=%v", 
		view, blockNumber, index, primary, valSet.Validators)
	return primary, nil
}

// updateValidators updates the collection based on the input and returns arrays of additions and deletions
// Note: validatorsHeight and validatorsBeatTime are preserved for future use (e.g., node status tracking)
func (valSet *validatorSet) updateValidators(validators []string) (addedValidators []string, removedValidators []string,
	err error) {
	valSet.Lock()
	defer valSet.Unlock()

	removedValidatorsMap := make(map[string]bool)
	for _, v := range valSet.Validators {
		removedValidatorsMap[v] = true
	}

	for _, v := range validators {
		// addedValidators
		if !valSet.hasValidator(v) {
			addedValidators = append(addedValidators, v)
		}

		delete(removedValidatorsMap, v)
	}

	// removedValidators
	for k := range removedValidatorsMap {
		removedValidators = append(removedValidators, k)
	}

	sort.SliceStable(validators, func(i, j int) bool {
		return validators[i] < validators[j]
	})

	valSet.Validators = validators

	// Clean up validatorsHeight and validatorsBeatTime for removed validators
	for _, removed := range removedValidators {
		delete(valSet.validatorsHeight, removed)
		delete(valSet.validatorsBeatTime, removed)
	}

	sort.SliceStable(addedValidators, func(i, j int) bool {
		return addedValidators[i] < addedValidators[j]
	})

	sort.SliceStable(removedValidators, func(i, j int) bool {
		return removedValidators[i] < removedValidators[j]
	})
	valSet.logger.Infof("%v update validators, validators: %v, addedValidators: %v, removedValidators: %v",
		valSet.Validators, validators, addedValidators, removedValidators)
	return
}

// getByIndex gets validator by index
func (valSet *validatorSet) getByIndex(index int32) (validator string, err error) {
	if valSet == nil {
		return "", ErrInvalidIndex
	}

	valSet.Lock()
	defer valSet.Unlock()

	if index < 0 || index >= int32(len(valSet.Validators)) {
		return "", ErrInvalidIndex
	}

	val := valSet.Validators[index]
	return val, nil
}

// getIndexByString gets index of validator
func (valSet *validatorSet) getIndexByString(validator string) int32 {
	if valSet == nil {
		return -1
	}

	valSet.Lock()
	defer valSet.Unlock()

	for i, val := range valSet.Validators {
		if val == validator {
			return int32(i)
		}
	}
	return -1
}
