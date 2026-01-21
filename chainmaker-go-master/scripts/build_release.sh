#!/usr/bin/env bash
#
# Copyright (C) BABEC. All rights reserved.
# Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.
#
# SPDX-License-Identifier: Apache-2.0
#

set -e

CURRENT_PATH=$(pwd)
PROJECT_PATH=$(dirname "${CURRENT_PATH}")
BUILD_PATH=${PROJECT_PATH}/build
RELEASE_PATH=${PROJECT_PATH}/build/release
BACKUP_PATH=${PROJECT_PATH}/build/backup
BUILD_CRYPTO_CONFIG_PATH=${BUILD_PATH}/crypto-config
BUILD_CONFIG_PATH=${BUILD_PATH}/config
VERSION=v2.3.7
DATETIME=$(date "+%Y%m%d%H%M%S")
PLATFORM=$(uname -m)
system=$(uname)

function xsed() {

    if [ "${system}" = "Linux" ]; then
        sed -i "$@"
    else
        sed -i '' "$@"
    fi
}

function check_env() {
    if  [ ! -d $BUILD_CONFIG_PATH ] ;then
        echo $BUILD_CONFIG_PATH" is missing"
        exit 1
    fi

    if  [ ! -d $BUILD_CRYPTO_CONFIG_PATH ] ;then
        echo $BUILD_CRYPTO_CONFIG_PATH" is missing"
        exit 1
    fi
}

function build() {
    cd $PROJECT_PATH
    echo "build chainmaker ${PROJECT_PATH}..."
    make
}

function package() {
    if [ -d $RELEASE_PATH ]; then
        mkdir -p $BACKUP_PATH/backup_release
        mv $RELEASE_PATH $BACKUP_PATH/backup_release/release_$(date "+%Y%m%d%H%M%S")
    fi

    mkdir -p $RELEASE_PATH
    cd $RELEASE_PATH
    echo "tar zcf crypto-config..."
    tar -zcf crypto-config-$DATETIME.tar.gz ../crypto-config

    # 检测是否是单组织多节点场景
    org_count=$(ls -1 $BUILD_CRYPTO_CONFIG_PATH 2>/dev/null | wc -l)
    node_count=$(ls -1d $BUILD_CONFIG_PATH/node* 2>/dev/null | wc -l)
    
    # 判断：只有一个组织，但有多个节点目录，且节点目录名是 node1, node2... 格式
    is_single_org_multi_node=false
    if [ $org_count -eq 1 ] && [ $node_count -gt 1 ]; then
        # 检查节点目录命名是否符合 node1, node2... 格式
        first_node=$(ls -1d $BUILD_CONFIG_PATH/node* 2>/dev/null | head -1 | xargs basename)
        if [[ $first_node =~ ^node[0-9]+$ ]]; then
            is_single_org_multi_node=true
            org_name=$(ls -1 $BUILD_CRYPTO_CONFIG_PATH | head -1)
            echo "Detected single org multi-node scenario: org=$org_name, nodes=$node_count"
        fi
    fi

    c=0
    dirNames[0]=""
    
    if [ "$is_single_org_multi_node" = true ]; then
        # 单组织多节点场景：为每个节点创建独立的发布包
        org_name=$(ls -1 $BUILD_CRYPTO_CONFIG_PATH | head -1)
        for node_dir in `ls -1d $BUILD_CONFIG_PATH/node* | sort -V`
        do
            node_name=$(basename $node_dir)
            node_num=${node_name#node}  # 提取节点编号
            
            chainmaker_file=chainmaker-$VERSION-${org_name}-${node_name}
            dirNames[$c]=$chainmaker_file
            c=$(($c+1))
            mkdir $chainmaker_file
            mkdir $chainmaker_file/bin
            mkdir $chainmaker_file/lib
            # 对于单组织多节点，保持 node1, node2... 目录结构以匹配配置路径
            mkdir -p $chainmaker_file/config/$node_name
            mkdir $chainmaker_file/log
            cp $PROJECT_PATH/bin/chainmaker   $chainmaker_file/bin
            cp $CURRENT_PATH/bin/start.sh     $chainmaker_file/bin
            cp $CURRENT_PATH/bin/stop.sh      $chainmaker_file/bin
            cp $CURRENT_PATH/bin/restart.sh   $chainmaker_file/bin
            cp $CURRENT_PATH/bin/version.sh   $chainmaker_file/bin
            cp $CURRENT_PATH/bin/docker-vm-standalone-start.sh   $chainmaker_file/bin
            cp $CURRENT_PATH/bin/docker-vm-standalone-stop.sh   $chainmaker_file/bin
            cp $CURRENT_PATH/service/*        $chainmaker_file/bin
            if [ "${system}" = "Linux" ]; then
              cp -r $PROJECT_PATH/main/libwasmer_runtime_c_api.so     $chainmaker_file/lib/libwasmer.so
              cp -r $PROJECT_PATH/main/prebuilt/linux/wxdec           $chainmaker_file/lib/
            else
              cp -r $PROJECT_PATH/main/libwasmer.dylib                $chainmaker_file/lib/
              cp -r $PROJECT_PATH/main/prebuilt/mac/wxdec             $chainmaker_file/lib/
            fi
            chmod 644 $chainmaker_file/lib/*
            chmod 700 $chainmaker_file/lib/wxdec
            chmod 700 $chainmaker_file/bin/*
            # 复制节点配置，保持 node1, node2... 目录结构以匹配配置中的路径引用
            cp -r $node_dir/* $chainmaker_file/config/$node_name
            # 对于单组织多节点，{org_id} 应该替换为节点名（node1, node2...）以匹配配置路径
            xsed "s%{org_id}%$node_name%g"         $chainmaker_file/bin/start.sh
            xsed "s%{org_id}%$node_name%g"         $chainmaker_file/bin/stop.sh
            xsed "s%{org_id}%$node_name%g"         $chainmaker_file/bin/restart.sh
            xsed "s%{org_id}%$node_name%g"         $chainmaker_file/bin/run.sh
            d=$(date "+%Y%m%d%H%M%S")
            xsed "s%{tagName}%name-$d%g"         $chainmaker_file/bin/*.sh
            echo "tar zcf ${chainmaker_file}..."
            tar -zcf chainmaker-$VERSION-${org_name}-${node_name}-$DATETIME-$PLATFORM.tar.gz $chainmaker_file &
        done
    else
        # 多组织场景：保持原有逻辑
        for file in `ls -v $BUILD_CRYPTO_CONFIG_PATH`
        do
            chainmaker_file=chainmaker-$VERSION-$file
            dirNames[$c]=$chainmaker_file
            c=$(($c+1))
            mkdir $chainmaker_file
            mkdir $chainmaker_file/bin
            mkdir $chainmaker_file/lib
            mkdir -p $chainmaker_file/config/$file
            mkdir $chainmaker_file/log
            cp $PROJECT_PATH/bin/chainmaker   $chainmaker_file/bin
            cp $CURRENT_PATH/bin/start.sh     $chainmaker_file/bin
            cp $CURRENT_PATH/bin/stop.sh      $chainmaker_file/bin
            cp $CURRENT_PATH/bin/restart.sh   $chainmaker_file/bin
            cp $CURRENT_PATH/bin/version.sh   $chainmaker_file/bin
            cp $CURRENT_PATH/bin/docker-vm-standalone-start.sh   $chainmaker_file/bin
            cp $CURRENT_PATH/bin/docker-vm-standalone-stop.sh   $chainmaker_file/bin
            cp $CURRENT_PATH/service/*        $chainmaker_file/bin
            if [ "${system}" = "Linux" ]; then
              cp -r $PROJECT_PATH/main/libwasmer_runtime_c_api.so     $chainmaker_file/lib/libwasmer.so
              cp -r $PROJECT_PATH/main/prebuilt/linux/wxdec           $chainmaker_file/lib/
            else
              cp -r $PROJECT_PATH/main/libwasmer.dylib                $chainmaker_file/lib/
              cp -r $PROJECT_PATH/main/prebuilt/mac/wxdec             $chainmaker_file/lib/
            fi
            chmod 644 $chainmaker_file/lib/*
            chmod 700 $chainmaker_file/lib/wxdec
            chmod 700 $chainmaker_file/bin/*
            cp -r $BUILD_CONFIG_PATH/node$c/* $chainmaker_file/config/$file
            xsed "s%{org_id}%$file%g"         $chainmaker_file/bin/start.sh
            xsed "s%{org_id}%$file%g"         $chainmaker_file/bin/stop.sh
            xsed "s%{org_id}%$file%g"         $chainmaker_file/bin/restart.sh
            xsed "s%{org_id}%$file%g"         $chainmaker_file/bin/run.sh
            d=$(date "+%Y%m%d%H%M%S")
            xsed "s%{tagName}%name-$d%g"         $chainmaker_file/bin/*.sh
            echo "tar zcf ${chainmaker_file}..."
            tar -zcf chainmaker-$VERSION-$file-$DATETIME-$PLATFORM.tar.gz $chainmaker_file &
        done
    fi
    
    echo "wait tar..."
    wait
    for dirName in ${dirNames[@]}
    do
      # echo "rm -rf $PROJECT_PATH/build/release/$dirName"
      rm -rf $PROJECT_PATH/build/release/$dirName
    done
}

check_env
build
package

