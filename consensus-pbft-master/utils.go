/*
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.

SPDX-License-Identifier: Apache-2.0
*/

package pbft

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"strconv"
	"sync"
	"time"

	consensus_utils "chainmaker.org/chainmaker/consensus-utils/v2"
	"chainmaker.org/chainmaker/consensus-utils/v2/wal_service"
	"chainmaker.org/chainmaker/localconf/v2"
	"chainmaker.org/chainmaker/logger/v2"
	"chainmaker.org/chainmaker/lws"
	"chainmaker.org/chainmaker/pb-go/v2/common"
	"chainmaker.org/chainmaker/pb-go/v2/config"
	"chainmaker.org/chainmaker/pb-go/v2/consensus"
	pbftpb "chainmaker.org/chainmaker/pb-go/v2/consensus/pbft"
	"chainmaker.org/chainmaker/protocol/v2"
	"github.com/gogo/protobuf/proto"
)

// GetValidatorListFromConfig gets validator list from config
func GetValidatorListFromConfig(chainConfig *config.ChainConfig) (validators []string, err error) {
	nodes := chainConfig.Consensus.Nodes
	for _, node := range nodes {
		validators = append(validators, node.NodeId...)
	}
	return validators, nil
}

// VerifyBlockSignatures verifies whether the signatures in block
// is qualified with the consensus algorithm. It should return nil
// error when verify successfully, and return corresponding error
// when failed.
func VerifyBlockSignatures(chainConf protocol.ChainConf,
	ac protocol.AccessControlProvider, block *common.Block, store protocol.BlockchainStore,
	validatorListFunc consensus_utils.ValidatorListFunc) error {

	if block == nil || block.Header == nil || block.AdditionalData == nil || block.AdditionalData.ExtraData == nil {
		return fmt.Errorf("invalid block")
	}
	blockVoteSet, ok := block.AdditionalData.ExtraData[PBFTAdditionalDataKey]
	if !ok {
		return fmt.Errorf("block.AdditionalData.ExtraData[PBFTAdditionalDataKey] not exist")
	}

	// Unmarshal CommitVoteSet from block additional data
	// CommitVoteSet contains the commit votes that were used to commit this block
	commitVoteSetProto := new(pbftpb.CommitVoteSet)
	if err := proto.Unmarshal(blockVoteSet, commitVoteSetProto); err != nil {
		return err
	}

	height := block.Header.BlockHeight
	chainConfig, err := chainConf.GetChainConfigFromFuture(height)
	if err != nil {
		return err
	}

	validators, err := validatorListFunc(chainConfig, store)
	if err != nil {
		return err
	}

	logger := logger.GetLoggerByChain(logger.MODULE_CONSENSUS, chainConfig.ChainId)
	validatorSet := newValidatorSet(logger, validators)

	// Verify that the commit vote set matches the block hash
	if commitVoteSetProto.Digest != nil && !bytes.Equal(commitVoteSetProto.Digest, block.Header.BlockHash) {
		return fmt.Errorf("unmatch QC: %x to block hash: %v", commitVoteSetProto.Digest, block.Header.BlockHash)
	}

	// Verify commit votes signatures
	if commitVoteSetProto.Votes != nil {
		for nodeId, commit := range commitVoteSetProto.Votes {
			// Verify that the node is a valid validator
			if !validatorSet.HasValidator(commit.NodeId) {
				return fmt.Errorf("invalid validator in commit vote: %s", commit.NodeId)
			}
			if commit.Endorsement == nil {
				continue
			}
			commitCopy := &pbftpb.Commit{
				NodeId:   commit.NodeId,
				View:     commit.View,
				Sequence: commit.Sequence,
				Digest:   commit.Digest,
			}
			message := mustMarshal(commitCopy)
			principal, err := ac.CreatePrincipal(
				protocol.ResourceNameConsensusNode,
				[]*common.EndorsementEntry{commit.Endorsement},
				message,
			)
			if err != nil {
				logger.Debugf("VerifyBlockSignatures block (%d-%x) failed to create principal for %s: %v",
					block.Header.BlockHeight, block.Header.BlockHash, nodeId, err)
				return err
			}
			result, err := ac.VerifyMsgPrincipal(principal, block.GetHeader().GetBlockVersion())
			if err != nil {
				logger.Debugf("VerifyBlockSignatures block (%d-%x) failed to verify signature for %s: %v",
					block.Header.BlockHeight, block.Header.BlockHash, nodeId, err)
				return err
			}
			if !result {
				return fmt.Errorf("VerifyBlockSignatures block (%d-%x) signature verification failed for %s",
					block.Header.BlockHeight, block.Header.BlockHash, nodeId)
			}
		}
	}

	logger.Debugf("VerifyBlockSignatures block (%d-%x) success",
		block.Header.BlockHeight, block.Header.BlockHash)
	return nil
}

// VerifyRoundQc verifies whether the signatures in roundQC
// NOTE: This function is deprecated as RoundQC type is no longer available
// TODO: Refactor to use the new PBFT types if this functionality is still needed
func VerifyRoundQc(logger protocol.Logger, ac protocol.AccessControlProvider,
	validators *validatorSet, roundQC interface{}, blockVersion uint32) error {
	return fmt.Errorf("VerifyRoundQc is deprecated: RoundQC type is no longer available")
}

// VerifyQcFromVotes verifies whether the signatures in votes
// NOTE: This function is deprecated as Vote and VoteType types are no longer available
// TODO: Refactor to use Prepare/Commit types if this functionality is still needed
func VerifyQcFromVotes(logger protocol.Logger, vs interface{}, ac protocol.AccessControlProvider,
	validators *validatorSet, voteType interface{}, blockVersion uint32) (interface{}, error) {
	return nil, fmt.Errorf("VerifyQcFromVotes is deprecated: Vote and VoteType types are no longer available")
}

// GetValidatorList gets validator list from config
func GetValidatorList(chainConfig *config.ChainConfig, store protocol.BlockchainStore) (validators []string,
	err error) {
	if chainConfig.Consensus.Type == consensus.ConsensusType_PBFT {
		return GetValidatorListFromConfig(chainConfig)
	}
	return nil, fmt.Errorf("unknown consensus type: %s", chainConfig.Consensus.Type)
}

// InitLWS initializes LWS
func InitLWS(config *config.ConsensusConfig, chainId, nodeId string) (lwsInstance *lws.Lws,
	walWriteMode wal_service.WalWriteMode, err error) {
	for _, v := range config.ExtConfig {
		if v.Key == wal_service.WALWriteModeKey {
			val, conv_err := strconv.Atoi(v.Value)
			if conv_err != nil {
				return nil, wal_service.NonWalWrite, conv_err
			}
			walWriteMode = wal_service.WalWriteMode(val)
		}
	}

	waldir := path.Join(localconf.ChainMakerConfig.GetStorePath(), chainId,
		fmt.Sprintf("%s_%s", wal_service.WalDir, nodeId))
	// the max size of wal file is 64M
	// the max number of wal files is 3
	lwsInstance, err = lws.Open(waldir, lws.WithSegmentSize(1<<26), lws.WithFileLimitForPurge(3),
		lws.WithWriteFlag(lws.WF_SYNCFLUSH, 0))
	if err != nil {
		return nil, wal_service.NonWalWrite, err
	}
	return
}

// CurrentTime returns current time
func CurrentTime() time.Time {
	return time.Now()
}

// mustMarshal marshals protobuf message to byte slice or panic
func mustMarshal(msg proto.Message) (data []byte) {
	var err error
	defer func() {
		if recover() != nil {
			data, err = proto.Marshal(msg)
			if err != nil {
				panic(err)
			}
		}
	}()

	data, err = proto.Marshal(msg)
	if err != nil {
		panic(err)
	}
	return
}

// mustUnmarshal unmarshals from byte slice to protobuf message or panic
func mustUnmarshal(b []byte, msg proto.Message) {
	if err := proto.Unmarshal(b, msg); err != nil {
		panic(err)
	}
}

// verifyPrepareVotes verifies prepare votes signatures
func verifyPrepareVotes(votes map[string]*pbftpb.Prepare, ac protocol.AccessControlProvider, blockVersion uint32) error {
	if votes == nil {
		return errors.New("invalid votes")
	}

	wg := sync.WaitGroup{}
	var retErr error
	var retErrMutex sync.Mutex
	for _, value := range votes {
		wg.Add(1)
		go func(v *pbftpb.Prepare) {
			var err error
			defer func() {
				wg.Done()
				if err != nil {
					retErrMutex.Lock()
					if retErr == nil {
						retErr = err
					}
					retErrMutex.Unlock()
				}
			}()

			// Create a copy without endorsement for signing
			prepareCopy := &pbftpb.Prepare{
				NodeId:   v.NodeId,
				View:     v.View,
				Sequence: v.Sequence,
				Digest:   v.Digest,
			}
			message := mustMarshal(prepareCopy)

			if v.Endorsement == nil {
				err = fmt.Errorf("prepare vote missing endorsement from %s", v.NodeId)
				return
			}

			principal, err := ac.CreatePrincipal(
				protocol.ResourceNameConsensusNode,
				[]*common.EndorsementEntry{v.Endorsement},
				message,
			)
			if err != nil {
				return
			}

			result, err := ac.VerifyMsgPrincipal(principal, blockVersion)
			if err != nil {
				return
			}

			if !result {
				err = fmt.Errorf("prepare vote signature verification failed for %s", v.NodeId)
				return
			}
		}(value)
	}

	wg.Wait()
	return retErr
}

// verifyCommitVotes verifies commit votes signatures
func verifyCommitVotes(votes map[string]*pbftpb.Commit, ac protocol.AccessControlProvider, blockVersion uint32) error {
	if votes == nil {
		return errors.New("invalid votes")
	}

	wg := sync.WaitGroup{}
	var retErr error
	var retErrMutex sync.Mutex
	for _, value := range votes {
		wg.Add(1)
		go func(v *pbftpb.Commit) {
			var err error
			defer func() {
				wg.Done()
				if err != nil {
					retErrMutex.Lock()
					if retErr == nil {
						retErr = err
					}
					retErrMutex.Unlock()
				}
			}()

			// Create a copy without endorsement for signing
			commitCopy := &pbftpb.Commit{
				NodeId:   v.NodeId,
				View:     v.View,
				Sequence: v.Sequence,
				Digest:   v.Digest,
			}
			message := mustMarshal(commitCopy)

			if v.Endorsement == nil {
				err = fmt.Errorf("commit vote missing endorsement from %s", v.NodeId)
				return
			}

			principal, err := ac.CreatePrincipal(
				protocol.ResourceNameConsensusNode,
				[]*common.EndorsementEntry{v.Endorsement},
				message,
			)
			if err != nil {
				return
			}

			result, err := ac.VerifyMsgPrincipal(principal, blockVersion)
			if err != nil {
				return
			}

			if !result {
				err = fmt.Errorf("commit vote signature verification failed for %s", v.NodeId)
				return
			}
		}(value)
	}

	wg.Wait()
	return retErr
}
