/*
Copyright (C) BABEC. All rights reserved.
Copyright (C) THL A29 Limited, a Tencent company. All rights reserved.

SPDX-License-Identifier: Apache-2.0
*/

package blockchain

// Stop all the modules.
func (bc *Blockchain) Stop() {
	// stop all module

	// stop sequence：
	// 1、sync service
	// 2、core engine
	// 3、consensus module
	// 4、net service
	// 5、tx pool
	// 6、vm
	// 7、store

	var stopModules = make([]map[string]func() error, 0)

	if bc.isModuleStartUp(moduleNameStore) {
		stopModules = append(stopModules, map[string]func() error{moduleNameStore: bc.stopStore})
	}
	if bc.isModuleStartUp(moduleNameNetService) {
		stopModules = append(stopModules, map[string]func() error{moduleNameNetService: bc.stopNetService})
	}
	if bc.isModuleStartUp(moduleNameConsensus) {
		stopModules = append(stopModules, map[string]func() error{moduleNameConsensus: bc.stopConsensus})
	}
	if bc.isModuleStartUp(moduleNameVM) {
		stopModules = append(stopModules, map[string]func() error{moduleNameVM: bc.stopVM})
	}
	if bc.isModuleStartUp(moduleNameCore) {
		stopModules = append(stopModules, map[string]func() error{moduleNameCore: bc.stopCoreEngine})
	}
	if bc.isModuleStartUp(moduleNameSync) {
		stopModules = append(stopModules, map[string]func() error{moduleNameSync: bc.stopSyncService})
	}
	if bc.isModuleStartUp(moduleNameTxPool) {
		stopModules = append(stopModules, map[string]func() error{moduleNameTxPool: bc.stopTxPool})
	}

	total := len(stopModules)

	// stop with total order
	for idx := total - 1; idx >= 0; idx-- {
		stopModule := stopModules[idx]
		for name, stopFunc := range stopModule {
			if err := stopFunc(); err != nil {
				bc.log.Errorf("stop module[%s] failed, %s", name, err)
				continue
			}
			bc.log.Infof("STOP STEP (%d/%d) => stop module[%s] success :)", total-idx, total, name)
		}
	}
}

// Stop all the modules.
func (bc *Blockchain) StopWithoutVm() {
	// stop all modules except vm

	// stop sequence：
	// 1、sync service
	// 2、core engine
	// 3、consensus module
	// 4、net service
	// 5、tx pool
	// 6、store

	var stopModules = make([]map[string]func() error, 0)

	if bc.isModuleStartUp(moduleNameStore) {
		stopModules = append(stopModules, map[string]func() error{moduleNameStore: bc.stopStore})
	}
	if bc.isModuleStartUp(moduleNameNetService) {
		stopModules = append(stopModules, map[string]func() error{moduleNameNetService: bc.stopNetService})
	}
	if bc.isModuleStartUp(moduleNameConsensus) {
		stopModules = append(stopModules, map[string]func() error{moduleNameConsensus: bc.stopConsensus})
	}
	if bc.isModuleStartUp(moduleNameCore) {
		stopModules = append(stopModules, map[string]func() error{moduleNameCore: bc.stopCoreEngine})
	}
	if bc.isModuleStartUp(moduleNameSync) {
		stopModules = append(stopModules, map[string]func() error{moduleNameSync: bc.stopSyncService})
	}
	if bc.isModuleStartUp(moduleNameTxPool) {
		stopModules = append(stopModules, map[string]func() error{moduleNameTxPool: bc.stopTxPool})
	}

	total := len(stopModules)

	// stop with total order
	for idx := total - 1; idx >= 0; idx-- {
		stopModule := stopModules[idx]
		for name, stopFunc := range stopModule {
			if err := stopFunc(); err != nil {
				bc.log.Errorf("stop module[%s] failed, %s", name, err)
				continue
			}
			bc.log.Infof("STOP STEP (%d/%d) => stop module[%s] success :)", total-idx, total, name)
		}
	}
}

// StopOnRequirements close the module instance which is required to shut down when chain configuration updating.
func (bc *Blockchain) StopOnRequirements() {
	stopMethodMap := map[string]func() error{
		moduleNameNetService: bc.stopNetService,
		moduleNameSync:       bc.stopSyncService,
		moduleNameCore:       bc.stopCoreEngine,
		moduleNameConsensus:  bc.stopConsensus,
		moduleNameTxPool:     bc.stopTxPool,
		moduleNameVM:         bc.stopVM,
	}

	// stop sequence：
	// 1、sync service
	// 2、core engine
	// 3、consensus module
	// 4、net service
	// 5、tx pool

	sequence := map[string]int{
		moduleNameSync:       0,
		moduleNameCore:       1,
		moduleNameConsensus:  2,
		moduleNameNetService: 3,
		moduleNameTxPool:     4,
		moduleNameVM:         5,
	}
	closeFlagArray := [6]string{}
	// 防止死锁，先获取所有start的模块列表
	startModuleNames := make([]string, 0)
	bc.startModuleLock.RLock()
	for moduleName := range bc.startModules {
		startModuleNames = append(startModuleNames, moduleName)
	}
	bc.startModuleLock.RUnlock()
	// 遍历所有start的模块列表
	for _, moduleName := range startModuleNames {
		bc.initModuleLock.RLock()
		_, ok := bc.initModules[moduleName]
		bc.initModuleLock.RUnlock()
		if ok {
			continue
		}
		seq, canStop := sequence[moduleName]
		if canStop {
			closeFlagArray[seq] = moduleName
		}
	}
	//
	//for moduleName := range bc.startModules {
	//	bc.initModuleLock.RLock()
	//	_, ok := bc.initModules[moduleName]
	//	bc.initModuleLock.RUnlock()
	//	if ok {
	//		continue
	//	}
	//	seq, canStop := sequence[moduleName]
	//	if canStop {
	//		closeFlagArray[seq] = moduleName
	//	}
	//}
	// stop modules
	for i := range closeFlagArray {
		moduleName := closeFlagArray[i]
		if moduleName == "" {
			continue
		}
		stopFunc := stopMethodMap[moduleName]
		err := stopFunc()
		if err != nil {
			bc.log.Errorf("stop module[%s] failed, %s", moduleName, err)
			continue
		}
		bc.log.Infof("stop module[%s] success :)", moduleName)
	}
}

func (bc *Blockchain) stopNetService() error {
	// stop net service
	if err := bc.netService.Stop(); err != nil {
		bc.log.Errorf("stop net service failed, %s", err.Error())
		return err
	}
	bc.startModuleLock.Lock()
	defer bc.startModuleLock.Unlock()
	delete(bc.startModules, moduleNameNetService)
	return nil
}

func (bc *Blockchain) stopConsensus() error {
	// stop consensus module
	if err := bc.consensus.Stop(); err != nil {
		bc.log.Errorf("stop consensus failed, %s", err.Error())
		return err
	}
	bc.startModuleLock.Lock()
	defer bc.startModuleLock.Unlock()
	delete(bc.startModules, moduleNameConsensus)
	return nil
}

func (bc *Blockchain) stopCoreEngine() error {
	// stop core engine
	bc.coreEngine.Stop()
	bc.startModuleLock.Lock()
	defer bc.startModuleLock.Unlock()
	delete(bc.startModules, moduleNameCore)
	return nil
}

func (bc *Blockchain) stopSyncService() error {
	// stop sync
	bc.syncServer.Stop()
	bc.startModuleLock.Lock()
	defer bc.startModuleLock.Unlock()
	delete(bc.startModules, moduleNameSync)
	return nil
}

func (bc *Blockchain) stopTxPool() error {
	// stop tx pool
	err := bc.txPool.Stop()
	if err != nil {
		bc.log.Errorf("stop tx pool failed, %s", err)

		return err
	}
	bc.startModuleLock.Lock()
	defer bc.startModuleLock.Unlock()
	delete(bc.startModules, moduleNameTxPool)
	return nil
}

func (bc *Blockchain) stopVM() error {
	// stop vm
	err := bc.vmMgr.Stop()
	if err != nil {
		bc.log.Errorf("stop vm failed, %s", err)
		return err
	}
	bc.startModuleLock.Lock()
	defer bc.startModuleLock.Unlock()
	delete(bc.startModules, moduleNameVM)
	return nil
}

func (bc *Blockchain) stopStore() error {
	// stop store
	err := bc.store.Close()
	if err != nil {
		bc.log.Errorf("stop store failed, %s", err)

		return err
	}
	bc.startModuleLock.Lock()
	defer bc.startModuleLock.Unlock()
	delete(bc.startModules, moduleNameStore)
	return nil
}
