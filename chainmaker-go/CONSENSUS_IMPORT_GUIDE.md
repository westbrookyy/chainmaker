# 共识算法 Protobuf 导入指南

## 概述

在长安链中添加新的共识算法时，需要了解 protobuf 文件的导入模式和使用方式。

## 导入模式

### 1. 通用共识类型导入

**路径**: `chainmaker.org/chainmaker/pb-go/v2/consensus`

**用途**: 导入通用的共识类型定义，包括：
- `ConsensusType` 枚举（SOLO, TBFT, MAXBFT, RAFT, DPOS, PBFT 等）
- 通用的共识消息结构

**示例**:
```go
import consensusPb "chainmaker.org/chainmaker/pb-go/v2/consensus"

// 使用示例
consensusPb.ConsensusType_PBFT
consensusPb.ConsensusType_MAXBFT
```

**文件位置**: `pb-go-master/consensus/consensus.pb.go`

### 2. 特定共识算法的消息类型导入

**路径**: `chainmaker.org/chainmaker/pb-go/v2/consensus/{算法名}`

**用途**: 导入特定共识算法的专用消息类型

**示例**:
```go
// MaxBFT 特定消息类型
import "chainmaker.org/chainmaker/pb-go/v2/consensus/maxbft"

// 使用示例
proposal := &maxbft.BuildProposal{...}
```

**文件位置**: `pb-go-master/consensus/{算法名}/{算法名}.pb.go`

**当前支持的特定算法包**:
- `consensus/maxbft` - MaxBFT 特定消息（如 BuildProposal）
- `consensus/pbft` - PBFT 特定消息（如 PrePrepare, Prepare, Commit, ViewChange, NewView）
- `consensus/tbft` - TBFT 特定消息
- `consensus/dpos` - DPoS 特定消息
- `consensus/raft` - Raft 特定消息
- `consensus/abft` - ABFT 特定消息

## 当前使用情况

### 已使用的导入

1. **通用共识类型** (`consensus.pb.go`):
   - `chainmaker-go/main/component_registry.go` - 注册共识提供者
   - `chainmaker-go/module/consensus/consensus_provider.go` - 共识提供者管理
   - `chainmaker-go/module/consensus/consensus_verifier.go` - 区块签名验证
   - `chainmaker-go/module/blockchain/blockchain_init.go` - 区块链初始化
   - 以及其他多个文件

2. **MaxBFT 特定消息** (`consensus/maxbft`):
   - `chainmaker-go/module/core/maxbftmode/proposer/block_proposer_impl.go` - 使用 `maxbft.BuildProposal`
   - `chainmaker-go/module/core/maxbftmode/core_maxbftmode_impl.go` - 处理 MaxBFT 消息
   - `chainmaker-go/module/accesscontrol/cert_ac_subscriber_utils.go` - MaxBFT 配置处理

3. **PBFT 特定消息** (`consensus/pbft`):
   - **当前状态**: PBFT 的特定消息类型已在 `pb-go-master/consensus/pbft/pbft.pb.go` 中定义
   - **使用情况**: 目前在 `chainmaker-go` 中**尚未**直接导入使用
   - **定义的消息类型**: PrePrepare, Prepare, Commit, ViewChange, NewView, PBFTMsg 等

## 添加新共识算法的步骤

### 步骤 1: 在通用共识类型中添加枚举值

**文件**: `pb-go-master/consensus/consensus.pb.go`

在 `ConsensusType` 枚举中添加新的共识类型：
```go
const (
    ConsensusType_SOLO   ConsensusType = 0
    ConsensusType_TBFT   ConsensusType = 1
    // ... 其他类型
    ConsensusType_NEW_ALGO ConsensusType = 8  // 添加新类型
)
```

### 步骤 2: 创建特定算法的 Protobuf 文件（如果需要）

如果新共识算法需要特定的消息类型：

1. **创建目录**: `pb-go-master/consensus/{新算法名}/`
2. **创建文件**: `pb-go-master/consensus/{新算法名}/{新算法名}.pb.go`
3. **定义消息类型**: 在 protobuf 文件中定义算法特定的消息结构

### 步骤 3: 在 chainmaker-go 中使用导入

#### 3.1 通用类型导入（必需）

在任何需要判断共识类型的地方导入：
```go
import consensusPb "chainmaker.org/chainmaker/pb-go/v2/consensus"

// 使用
if consensusType == consensusPb.ConsensusType_NEW_ALGO {
    // ...
}
```

#### 3.2 特定消息类型导入（如果需要）

如果需要在代码中使用特定算法的消息类型：
```go
import "chainmaker.org/chainmaker/pb-go/v2/consensus/{新算法名}"

// 使用
msg := &{新算法名}.SomeMessageType{...}
```

### 步骤 4: 注册共识提供者

**文件**: `chainmaker-go/main/component_registry.go`

```go
import (
    newAlgo "chainmaker.org/chainmaker/consensus-{新算法名}/v2"
    consensusPb "chainmaker.org/chainmaker/pb-go/v2/consensus"
)

func init() {
    consensus.RegisterConsensusProvider(
        consensusPb.ConsensusType_NEW_ALGO,
        func(config *utils.ConsensusImplConfig) (protocol.ConsensusEngine, error) {
            return newAlgo.New(config)
        },
    )
}
```

### 步骤 5: 添加验证逻辑（如果需要）

**文件**: `chainmaker-go/module/consensus/consensus_verifier.go`

```go
case consensusPb.ConsensusType_NEW_ALGO:
    return newAlgo.VerifyBlockSignatures(chainConf, ac, block, store, newAlgo.GetValidatorList)
```

## PBFT 导入情况说明

### 当前状态

1. **通用类型已定义**: `ConsensusType_PBFT` 已在 `consensus.pb.go` 中定义
2. **特定消息已定义**: PBFT 特定消息类型已在 `pb-go-master/consensus/pbft/pbft.pb.go` 中定义
3. **导入情况**: 
   - ✅ 通用类型已导入使用（在多个文件中）
   - ❌ 特定消息类型**尚未**在 chainmaker-go 中导入使用

### 如果需要使用 PBFT 特定消息类型

如果需要在代码中使用 PBFT 的特定消息类型（如 PrePrepare, Prepare, Commit 等），需要添加导入：

```go
import pbftPb "chainmaker.org/chainmaker/pb-go/v2/consensus/pbft"

// 使用示例
prePrepare := &pbftPb.PrePrepare{
    Primary:  "node1",
    View:     1,
    Sequence: 100,
    // ...
}
```

## 注意事项

1. **导入路径格式**: 
   - 通用类型: `chainmaker.org/chainmaker/pb-go/v2/consensus`
   - 特定算法: `chainmaker.org/chainmaker/pb-go/v2/consensus/{算法名}`

2. **包名约定**: 
   - 通用包名通常使用别名: `consensusPb` 或 `consensuspb`
   - 特定算法包名通常使用算法名: `maxbft`, `pbft`, `tbft` 等

3. **文件结构**: 
   - `pb-go-master/consensus/consensus.pb.go` - 通用类型
   - `pb-go-master/consensus/{算法名}/{算法名}.pb.go` - 特定算法消息

4. **依赖关系**: 
   - 特定算法的 protobuf 文件可以引用通用类型
   - 通用类型文件不依赖特定算法文件

## 相关文件清单

### pb-go-master 中的文件
- `consensus/consensus.pb.go` - 通用共识类型
- `consensus/pbft/pbft.pb.go` - PBFT 特定消息
- `consensus/maxbft/maxbft.pb.go` - MaxBFT 特定消息
- `consensus/tbft/tbft.pb.go` - TBFT 特定消息
- `consensus/dpos/dpos.pb.go` - DPoS 特定消息
- `consensus/raft/raft.pb.go` - Raft 特定消息
- `consensus/abft/abft.pb.go` - ABFT 特定消息

### chainmaker-go 中使用共识 protobuf 的主要文件
- `main/component_registry.go` - 注册共识提供者
- `module/consensus/consensus_provider.go` - 共识提供者管理
- `module/consensus/consensus_verifier.go` - 区块签名验证
- `module/blockchain/blockchain_init.go` - 区块链初始化
- `module/core/maxbftmode/**` - MaxBFT 模式实现（使用 maxbft 特定消息）
- `module/core/syncmode/pbft_provider.go` - PBFT 提供者（当前未使用 pbft 特定消息）
