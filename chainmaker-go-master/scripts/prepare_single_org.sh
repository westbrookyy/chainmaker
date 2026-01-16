#!/usr/bin/env bash
#
# Copyright (C) BABEC. All rights reserved.
# Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.
#
# SPDX-License-Identifier: Apache-2.0
#
# 单组织、多节点、单链配置生成脚本
# 用于生成一个组织下的多个节点，所有节点参与同一条链

# check mac gun-getopt
function checkEnv() {
  if [ "$(uname)" == "Darwin" ];then
    getopt --test
    if [ "$?" != "4" ];then
      brew -v > /dev/null
      if [ "$?" != "0" ];then
        echo 'Please install brew for Mac: ruby -e "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/master/install)"'
      fi
      echo 'Please install gnu-getopt for Mac: brew install gnu-getopt and set to PATH (brew link --force gnu-getopt)'
      exit
    fi
  fi
}
checkEnv

set -e

VERSION='"2030700"'

NODE_CNT=$1
P2P_PORT=$2
RPC_PORT=$3
VM_GO_RUNTIME_PORT=$4
VM_GO_ENGINE_PORT=$5

CURRENT_PATH=$(pwd)
PROJECT_PATH=$(dirname "${CURRENT_PATH}")
BUILD_PATH=${PROJECT_PATH}/build
CONFIG_TPL_PATH=${PROJECT_PATH}/config/config_tpl
BUILD_CRYPTO_CONFIG_PATH=${BUILD_PATH}/crypto-config
BUILD_CONFIG_PATH=${BUILD_PATH}/config
CRYPTOGEN_TOOL_PATH=${PROJECT_PATH}/tools/chainmaker-cryptogen
CRYPTOGEN_TOOL_BIN=${CRYPTOGEN_TOOL_PATH}/bin/chainmaker-cryptogen
CRYPTOGEN_TOOL_CONF=${CRYPTOGEN_TOOL_PATH}/config/crypto_config_template.yml
CRYPTOGEN_TOOL_PKCS11_KEYS=${CRYPTOGEN_TOOL_PATH}/config/hsm_keys.yml

# 单组织配置
ORG_ID="wx-org.chainmaker.org"
ORG_PATH="wx-org"

function show_help() {
    echo "Usage:  "
    echo "    prepare_single_org.sh node_cnt(2-16) p2p_port(default:11301) rpc_port(default:12301)"
    echo "               vm_go_runtime_port(default:32351) vm_go_engine_port(default:22351)"
    echo "               -c consense-type: 0-SOLO,1-TBFT,3-MAXBFT,4-RAFT "
    echo "               -l log-level: DEBUG,INFO,WARN,ERROR"
    echo "               -v docker-vm-enable: true,false"
    echo "               -h show help"
    echo "    eg1: prepare_single_org.sh 4"
    echo "    eg2: prepare_single_org.sh 4 11301 12301"
    echo "    eg3: prepare_single_org.sh 4 11301 12301 32351 22351"
    echo "    eg4: prepare_single_org.sh 4 11301 12301 32351 22351 -c 1 -l INFO -v false"
}

if ( [ $# -eq 1 ] && [ "$1" ==  "-h" ] ) ; then
    show_help
    exit 1
fi

if [ $# -eq 0 ]; then
    echo "invalid params"
    show_help
    exit 1
fi

function xsed() {
    system=$(uname)

    if [ "${system}" = "Linux" ]; then
        sed -i "$@"
    else
        sed -i '' "$@"
    fi
}

function check_params() {
    echo "begin check params..."
    if  [[ ! -n $NODE_CNT ]] ;then
        echo "node cnt is empty"
        show_help
        exit 1
    fi

    if  [ $NODE_CNT -lt 2 ] || [ $NODE_CNT -gt 16 ];then
        echo "node cnt should be 2 - 16 for single org multi-node setup"
        show_help
        exit 1
    fi

    # 判断是否是数字
    if [ "$P2P_PORT" -gt 0 ] 2>/dev/null ;then
      # 判断数字范围
      if  [ ${P2P_PORT} -ge 60000 ] || [ ${P2P_PORT} -le 10000 ];then
        P2P_PORT=11301
      fi
    else
        P2P_PORT=11301
    fi
    echo "param P2P_PORT $P2P_PORT"

    if [ "$RPC_PORT" -gt 0 ] 2>/dev/null ;then
      if  [ ${RPC_PORT} -ge 60000 ] || [ ${RPC_PORT} -le 10000 ];then
        RPC_PORT=12301
      fi
    else
        RPC_PORT=12301
    fi
    echo "param RPC_PORT $RPC_PORT"

    if [ "$VM_GO_RUNTIME_PORT" -gt 0 ] 2>/dev/null ;then
      if  [ ${VM_GO_RUNTIME_PORT} -ge 60000 ] || [ ${VM_GO_RUNTIME_PORT} -le 10000 ];then
        VM_GO_RUNTIME_PORT=32351
      fi
    else
        VM_GO_RUNTIME_PORT=32351
    fi
    echo "param VM_GO_RUNTIME_PORT $VM_GO_RUNTIME_PORT"

    if [ "$VM_GO_ENGINE_PORT" -gt 0 ] 2>/dev/null ;then
      if  [ ${VM_GO_ENGINE_PORT} -ge 60000 ] || [ ${VM_GO_ENGINE_PORT} -le 10000 ];then
        VM_GO_ENGINE_PORT=22351
      fi
    else
        VM_GO_ENGINE_PORT=22351
    fi
    echo "param VM_GO_ENGINE_PORT $VM_GO_ENGINE_PORT"
}

function generate_certs() {
    echo "begin generate certs for single org with ${NODE_CNT} nodes..."
    mkdir -p ${BUILD_PATH}
    cd "${BUILD_PATH}"
    if [ -d crypto-config ]; then
        mkdir -p backup/backup_certs
        mv crypto-config  backup/backup_certs/crypto-config_$(date "+%Y%m%d%H%M%S")
    fi

    # 使用模板并修改为单组织多节点配置
    if [ ! -f "$CRYPTOGEN_TOOL_CONF" ]; then
        echo "Error: Certificate config template not found: $CRYPTOGEN_TOOL_CONF"
        exit 1
    fi

    # 读取原始配置模板并创建单组织多节点配置
    echo "Reading original crypto config template..."
    
    # 复制模板
    cp $CRYPTOGEN_TOOL_CONF crypto_config.yml
    
    # 修改组织数量为1（替换第一个 count）
    xsed "0,/^[[:space:]]*count:/s/^\([[:space:]]*\)count: [0-9]*/\1count: 1/" crypto_config.yml
    
    # 检查并添加 node 配置
    if ! grep -q "^[[:space:]]*node:" crypto_config.yml; then
        # 找到 count: 1 所在行，在其后插入 node 配置
        # 使用 Python 来处理（更可靠）
        python3 <<PYTHON_SCRIPT
import sys

NODE_CNT = ${NODE_CNT}

with open('crypto_config.yml', 'r') as f:
    lines = f.readlines()

# 找到 count: 1 所在的行
insert_pos = -1
for i, line in enumerate(lines):
    if 'count: 1' in line and insert_pos == -1:
        insert_pos = i + 1
        break

if insert_pos > 0:
    # 在 count: 1 之后插入 node 配置
    lines.insert(insert_pos, '    node:\n')
    lines.insert(insert_pos + 1, '      count: {}    # 该组织下有 {} 个节点\n'.format(NODE_CNT, NODE_CNT))
    
    with open('crypto_config.yml', 'w') as f:
        f.writelines(lines)
else:
    print("Warning: Could not find count: 1 line", file=sys.stderr)
    sys.exit(1)
PYTHON_SCRIPT
    else
        # 如果已有 node 配置，修改其 count
        xsed "/^[[:space:]]*node:/,/^[[:space:]]*[a-z_]/ s/\([[:space:]]*\)count: [0-9]*/\1count: ${NODE_CNT}/" crypto_config.yml
    fi
    
    # 显示生成的配置（用于调试）
    echo "Generated crypto config for single org with ${NODE_CNT} nodes:"
    cat crypto_config.yml
    echo "---"

    if [ -f "$CRYPTOGEN_TOOL_PKCS11_KEYS" ]; then
        cp $CRYPTOGEN_TOOL_PKCS11_KEYS hsm_keys.yml
        ${CRYPTOGEN_TOOL_BIN} generate -c ./crypto_config.yml -p ./hsm_keys.yml
    else
        ${CRYPTOGEN_TOOL_BIN} generate -c ./crypto_config.yml
    fi
    
    # 检查证书生成结果
    echo "Checking generated certificates..."
    if [ -d "$BUILD_CRYPTO_CONFIG_PATH/$ORG_ID" ]; then
        echo "Organization directory found: $BUILD_CRYPTO_CONFIG_PATH/$ORG_ID"
        if [ -d "$BUILD_CRYPTO_CONFIG_PATH/$ORG_ID/node" ]; then
            echo "Node certificates found:"
            ls -la "$BUILD_CRYPTO_CONFIG_PATH/$ORG_ID/node" || true
        else
            echo "Warning: node directory not found"
        fi
    else
        echo "Error: Organization directory not found: $BUILD_CRYPTO_CONFIG_PATH/$ORG_ID"
        echo "Available directories:"
        ls -la "$BUILD_CRYPTO_CONFIG_PATH" || true
        exit 1
    fi
}

function generate_config() {
    LOG_LEVEL="" # default INFO
    CONSENSUS_TYPE=-1 # default 1
    MONITOR_PORT=14321
    PPROF_PORT=24321
    TRUSTED_PORT=13301
    VM_GO_CONTAINER_NAME_PREFIX="chainmaker-vm-go-container"
    ENABLE_VM_GO="" # default false

    set -- $(getopt -u -o c:l:v: "$@")
    while [ -n "$1" ]; do
        case "$1" in
            -c) CONSENSUS_TYPE=$2
                shift ;;
            -l) LOG_LEVEL=$2
                shift ;;
            -v) ENABLE_VM_GO=$2
                shift
        esac
        shift
    done

    # set CONSENSUS_TYPE
    if [ $CONSENSUS_TYPE == -1 ] ;then
      read -p "input consensus type (0-SOLO,1-TBFT(default),3-MAXBFT,4-RAFT): " tmp
      if  [ ! -z "$tmp" ] ;then
        if  [ $tmp -eq 0 ] || [ $tmp -eq 1 ] || [ $tmp -eq 3 ] || [ $tmp -eq 4 ] ;then
          CONSENSUS_TYPE=$tmp
        else
          echo "unknown consensus type [" $tmp "], so use default"
        fi
      fi
    fi
    if [ $CONSENSUS_TYPE == -1 ] ;then
          CONSENSUS_TYPE=1
    fi
    if [ $CONSENSUS_TYPE == 3 ] && [ $NODE_CNT -lt 4 ] ;then
      echo  "the current version of maxbft does not support the deployment of less than four nodes"
      exit
    fi
    if [ $CONSENSUS_TYPE == 0 ] && [ $NODE_CNT -gt 1 ] ;then
      echo  "SOLO consensus only supports single node"
      exit
    fi
    echo "param CONSENSUS_TYPE $CONSENSUS_TYPE"

    # set LOG_LEVEL
    if [ "$LOG_LEVEL" == "" ] ;then
      read -p "input log level (DEBUG|INFO(default)|WARN|ERROR): " tmp
      if  [ ! -z "$tmp" ] ;then
        if  [ $tmp == "DEBUG" ] || [ $tmp == "INFO" ] || [ $tmp == "WARN" ] || [ $tmp == "ERROR" ];then
            LOG_LEVEL=$tmp
        else
          echo "unknown log level [" $tmp "], so use default"
        fi
      fi
    fi
    if [ "$LOG_LEVEL" == "" ] ;then
        LOG_LEVEL="INFO"
    fi
    echo "param LOG_LEVEL $LOG_LEVEL"

    # set ENABLE_VM_GO
    if [ "$ENABLE_VM_GO" == "" ] ;then
      read -p "enable vm go (YES|NO(default))" enable_vm_go
      if  [ ! -z "$enable_vm_go" ]; then
        if  [ $enable_vm_go == "yes" ] || [ $enable_vm_go == "YES" ]; then
            ENABLE_VM_GO="true"
        else
            echo "disable vm go"
        fi
      fi
    fi
    if [ "$ENABLE_VM_GO" == "" ] ;then
      ENABLE_VM_GO="false"
    fi
    echo "param ENABLE_VM_GO $ENABLE_VM_GO"
    echo

    cd "${BUILD_PATH}"
    if [ -d config ]; then
        mkdir -p backup/backup_config
        mv config  backup/backup_config/config_$(date "+%Y%m%d%H%M%S")
    fi

    mkdir -p ${BUILD_PATH}/config
    cd ${BUILD_PATH}/config

    # 检查证书目录
    if [ ! -d "$BUILD_CRYPTO_CONFIG_PATH/$ORG_ID" ]; then
        echo "Error: Certificate directory not found: $BUILD_CRYPTO_CONFIG_PATH/$ORG_ID"
        echo "Please run generate_certs first"
        exit 1
    fi

    echo "config node total $NODE_CNT"
    
    # 检查证书目录结构
    echo "Checking certificate directory structure..."
    echo "Certificate path: $BUILD_CRYPTO_CONFIG_PATH/$ORG_ID"
    if [ ! -d "$BUILD_CRYPTO_CONFIG_PATH/$ORG_ID" ]; then
        echo "Error: Organization directory not found: $BUILD_CRYPTO_CONFIG_PATH/$ORG_ID"
        exit 1
    fi
    
    if [ ! -d "$BUILD_CRYPTO_CONFIG_PATH/$ORG_ID/node" ]; then
        echo "Error: Node directory not found: $BUILD_CRYPTO_CONFIG_PATH/$ORG_ID/node"
        exit 1
    fi
    
    # 列出所有可用的节点证书目录
    echo "Available node certificate directories:"
    ls -1 "$BUILD_CRYPTO_CONFIG_PATH/$ORG_ID/node" || true
    
    # 收集所有节点的 peerid
    # 首先查找所有可用的节点证书目录
    node_dirs=($(ls -1 "$BUILD_CRYPTO_CONFIG_PATH/$ORG_ID/node" 2>/dev/null | grep -E "^consensus[0-9]+$" | sort -V))
    if [ ${#node_dirs[@]} -eq 0 ]; then
        echo "Error: No consensus node directories found"
        echo "Available directories in node folder:"
        ls -la "$BUILD_CRYPTO_CONFIG_PATH/$ORG_ID/node" || true
        exit 1
    fi
    
    if [ ${#node_dirs[@]} -lt $NODE_CNT ]; then
        echo "Error: Expected $NODE_CNT nodes, but only found ${#node_dirs[@]} node directories"
        echo "Found directories: ${node_dirs[@]}"
        exit 1
    fi
    
    declare -a PEER_IDS
    for ((i = 1; i <= $NODE_CNT; i = i + 1)); do
        node_dir="${node_dirs[$((i-1))]}"
        node_cert_path="$BUILD_CRYPTO_CONFIG_PATH/$ORG_ID/node/$node_dir/$node_dir.nodeid"
        if [ -f "$node_cert_path" ]; then
            PEER_IDS[$i]=$(cat $node_cert_path)
            echo "Found node $i (directory: $node_dir) peerid: ${PEER_IDS[$i]}"
        else
            echo "Error: Node $i certificate not found at: $node_cert_path"
            echo "Available .nodeid files:"
            find "$BUILD_CRYPTO_CONFIG_PATH/$ORG_ID/node/$node_dir" -name "*.nodeid" 2>/dev/null || true
            exit 1
        fi
    done

    # 为每个节点生成配置
    for ((i = 1; i <= $NODE_CNT; i = i + 1)); do
        echo "begin generate node$i config..."
        mkdir -p ${BUILD_PATH}/config/node$i
        mkdir -p ${BUILD_PATH}/config/node$i/chainconfig
        cp $CONFIG_TPL_PATH/log.tpl node$i/log.yml
        xsed "s%{log_level}%$LOG_LEVEL%g" node$i/log.yml
        cp $CONFIG_TPL_PATH/chainmaker.tpl node$i/chainmaker.yml

        xsed "s%{net_port}%$(($P2P_PORT+$i-1))%g" node$i/chainmaker.yml
        xsed "s%{rpc_port}%$(($RPC_PORT+$i-1))%g" node$i/chainmaker.yml
        xsed "s%{monitor_port}%$(($MONITOR_PORT+$i-1))%g" node$i/chainmaker.yml
        xsed "s%{pprof_port}%$(($PPROF_PORT+$i-1))%g" node$i/chainmaker.yml
        xsed "s%{trusted_port}%$(($TRUSTED_PORT+$i-1))%g" node$i/chainmaker.yml
        xsed "s%{enable_vm_go}%$ENABLE_VM_GO%g" node$i/chainmaker.yml
        xsed "s%{dockervm_container_name}%"${VM_GO_CONTAINER_NAME_PREFIX}$i"%g" node$i/chainmaker.yml
        xsed "s%{vm_go_runtime_port}%$(($VM_GO_RUNTIME_PORT+$i-1))%g" node$i/chainmaker.yml
        xsed "s%{vm_go_engine_port}%$(($VM_GO_ENGINE_PORT+$i-1))%g" node$i/chainmaker.yml

        # 设置组织信息
        xsed "s%{org_id}%$ORG_ID%g" node$i/chainmaker.yml
        # 对于单组织多节点，每个节点使用自己的目录 node$i
        xsed "s%{org_path}%node$i%g" node$i/chainmaker.yml

        # 设置节点证书路径（单组织多节点，使用实际检测到的节点目录名）
        node_dir="${node_dirs[$((i-1))]}"
        xsed "s%{node_cert_path}%node\/$node_dir\/$node_dir.sign%g" node$i/chainmaker.yml
        xsed "s%{net_cert_path}%node\/$node_dir\/$node_dir.tls%g" node$i/chainmaker.yml
        xsed "s%{rpc_cert_path}%node\/$node_dir\/$node_dir.tls%g" node$i/chainmaker.yml

        # 启用单链配置
        xsed "s%#\(.*\)- chainId: chain1%\1- chainId: chain1%g" node$i/chainmaker.yml
        xsed "s%#\(.*\)genesis: ../config/{org_path1}/chainconfig/bc1.yml%\1genesis: ../config/node$i/chainconfig/bc1.yml%g" node$i/chainmaker.yml

        # 配置 seeds（所有节点的 peerid）
        system=$(uname)
        if [ "${system}" = "Linux" ]; then
            for ((k = 1; k <= $NODE_CNT; k = k + 1)); do
                if [ $k -ne $i ]; then
                    xsed "/  seeds:/a\    - \"/ip4/127.0.0.1/tcp/$(($P2P_PORT+$k-1))/p2p/${PEER_IDS[$k]}\"" node$i/chainmaker.yml
                fi
            done
        else
            ver=$(sw_vers | grep ProductVersion | cut -d':' -f2 | sed 's/\t//g')
            version=${ver:0:2}
            if [ $version -ge 11 ]; then
                for ((k = 1; k <= $NODE_CNT; k = k + 1)); do
                    if [ $k -ne $i ]; then
                        xsed  "/  seeds:/a\\
    - \"/ip4/127.0.0.1/tcp/$(($P2P_PORT+$k-1))/p2p/${PEER_IDS[$k]}\"\\
" node$i/chainmaker.yml
                    fi
                done
            else
                for ((k = 1; k <= $NODE_CNT; k = k + 1)); do
                    if [ $k -ne $i ]; then
                        xsed  "/  seeds:/a\\
                  \ \ \ \ - \"/ip4/127.0.0.1/tcp/$(($P2P_PORT+$k-1))/p2p/${PEER_IDS[$k]}\"\\
                  " node$i/chainmaker.yml
                    fi
                done
            fi
        fi

        # 生成链配置文件（单组织多节点）
        cp $CONFIG_TPL_PATH/chainconfig/bc_solo.tpl node$i/chainconfig/bc1.yml

        xsed "s%{consensus_type}%$CONSENSUS_TYPE%g" node$i/chainconfig/bc1.yml
        xsed "s%{chain_id}%chain1%g" node$i/chainconfig/bc1.yml
        xsed "s%{version}%$VERSION%g" node$i/chainconfig/bc1.yml

        # 先替换所有 {org_path} 占位符（必须在替换其他占位符之前）
        xsed "s%{org_path}%node$i%g" node$i/chainconfig/bc1.yml

        # 配置共识节点列表（单组织多节点）
        # 替换 org1_id
        xsed "s%{org1_id}%$ORG_ID%g" node$i/chainconfig/bc1.yml
        
        # 替换第一个节点ID
        xsed "s%{org1_peerid}%${PEER_IDS[1]}%g" node$i/chainconfig/bc1.yml
        
        # 添加其他节点ID（从第二个节点开始）
        if [ $NODE_CNT -gt 1 ]; then
            if [ "${system}" = "Linux" ]; then
                for ((k = 2; k <= $NODE_CNT; k = k + 1)); do
                    xsed "/        - \"${PEER_IDS[1]}\"/a\        - \"${PEER_IDS[$k]}\"" node$i/chainconfig/bc1.yml
                done
            else
                for ((k = 2; k <= $NODE_CNT; k = k + 1)); do
                    xsed "/        - \"${PEER_IDS[1]}\"/a\\
        - \"${PEER_IDS[$k]}\"\\
" node$i/chainconfig/bc1.yml
                done
            fi
        fi

        # 信任根证书路径应该已经通过上面的 {org_path} 替换完成了
        # 但为了确保，再次检查并替换（如果还有未替换的）
        xsed "s%\.\./config/{org_path}%../config/node$i%g" node$i/chainconfig/bc1.yml

        # 复制证书文件
        echo "begin node$i cert config..."
        mkdir -p $BUILD_CONFIG_PATH/node$i/certs/ca/$ORG_ID
        cp $BUILD_CRYPTO_CONFIG_PATH/$ORG_ID/ca/ca.crt $BUILD_CONFIG_PATH/node$i/certs/ca/$ORG_ID

        # 复制节点证书
        mkdir -p $BUILD_CONFIG_PATH/node$i/certs/node
        cp -r $BUILD_CRYPTO_CONFIG_PATH/$ORG_ID/node $BUILD_CONFIG_PATH/node$i/certs
        cp -r $BUILD_CRYPTO_CONFIG_PATH/$ORG_ID/user $BUILD_CONFIG_PATH/node$i/certs

        echo "node$i config generated successfully"
    done

    echo
    echo "=========================================="
    echo "Single org multi-node config generated!"
    echo "Organization: $ORG_ID"
    echo "Node count: $NODE_CNT"
    echo "Chain: chain1"
    echo "Config path: $BUILD_CONFIG_PATH"
    echo "=========================================="
}

check_params
generate_certs
generate_config $@
