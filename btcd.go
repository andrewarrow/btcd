// Copyright (c) 2013-2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/btcsuite/btcd/blockchain/indexers"
	"github.com/btcsuite/btcd/limits"
)

var (
	cfg *config
)

// winServiceMain is only invoked on Windows.  It detects when btcd is running
// as a service and reacts accordingly.
var winServiceMain func() (bool, error)

// btcdMain is the real main function for btcd.  It is necessary to work around
// the fact that deferred functions do not run when os.Exit() is called.  The
// optional serverChan parameter is mainly used by the service code to be
// notified with the server once it is setup so it can gracefully stop it when
// requested from the service control manager.
func btcdMain(serverChan chan<- *server) error {
	// Load configuration and parse command line.  This function also
	// initializes logging and configures it accordingly.
	tcfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	cfg = tcfg
	defer backendLog.Flush()

	// Get a channel that will be closed when a shutdown signal has been
	// triggered either from an OS signal such as SIGINT (Ctrl+C) or from
	// another subsystem such as the RPC server.
	interruptedChan := interruptListener()
	defer btcdLog.Info("Shutdown complete")

	// Show version at startup.
	btcdLog.Infof("Version %s", version())

	// Return now if an interrupt signal was triggered.
	if interruptRequested(interruptedChan) {
		return nil
	}
	// Load the block database.
	db, err := loadBlockDB()
	if err != nil {
		btcdLog.Errorf("%v", err)
		return err
	}
	defer func() {
		// Ensure the database is sync'd and closed on shutdown.
		btcdLog.Infof("Gracefully shutting down the database...")
		db.Close()
	}()

	// Return now if an interrupt signal was triggered.
	if interruptRequested(interruptedChan) {
		return nil
	}

	// Drop indexes and exit if requested.
	//
	// NOTE: The order is important here because dropping the tx index also
	// drops the address index since it relies on it.
	if cfg.DropAddrIndex {
		if err := indexers.DropAddrIndex(db); err != nil {
			btcdLog.Errorf("%v", err)
			return err
		}

		return nil
	}
	if cfg.DropTxIndex {
		if err := indexers.DropTxIndex(db); err != nil {
			btcdLog.Errorf("%v", err)
			return err
		}

		return nil
	}

	// Create server and start it.
	server, err := newServer(cfg.Listeners, db, activeNetParams.Params)
	if err != nil {
		// TODO: this logging could do with some beautifying.
		btcdLog.Errorf("Unable to start server on %v: %v",
			cfg.Listeners, err)
		return err
	}
	defer func() {
		btcdLog.Infof("Gracefully shutting down the server...")
		server.Stop()
		server.WaitForShutdown()
		srvrLog.Infof("Server shutdown complete")
	}()
	server.Start()
	if serverChan != nil {
		serverChan <- server
	}

	// Wait until the interrupt signal is received from an OS signal or
	// shutdown is requested through one of the subsystems such as the RPC
	// server.
	<-interruptedChan
	return nil
}

/*
4104be11746c7fa7e3f2b46057db84da5f42a0416e67f3297ea8adf7d3006069ce6f9eb902c702a5b68639d7e26fe60983677b8d3d9f100661205c10bb78c486e308ac

1 0: BTC 0.500000 to 76a914da90c671eb03bfddf1d62dd1bdf520f060cf41c088ac

1 1: BTC 9.500000 to 76a91483c89f3972a02540a7783bc630349303e41c148188ac

2 0: BTC 100.138033 to 76a91407b48fb827158183cf79ed7dd20bf5a992b83c7d88ac

3 0: BTC 0.010061 to 76a9146b79bacb72a14abe12694489e3680b261bfc27b188ac

3 1: BTC 40.000000 to 76a914e8a93cf5bdd2a0298406d225484dbfe100cf07b588ac

4 0: BTC 0.010000 to 76a914c097228cf6fb88d8f10191ce320d02c8a471760d88ac

4 1: BTC 10.940000 to 76a91476b57630f3142bfd25e5cbdad0b684c456b33ddf88ac

5 0: BTC 0.010000 to 76a9148569bc2b41b8e47db044fe50c93511391069d95188ac
*/
func foo() {
	h := "76a914da90c671eb03bfddf1d62dd1bdf520f060cf41c088ac"
	data, err := hex.DecodeString(h)
	fmt.Println(data, err)
	os.Exit(1)
}
func main() {
	foo()
	// Use all processor cores.
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Block and transaction processing can cause bursty allocations.  This
	// limits the garbage collector from excessively overallocating during
	// bursts.  This value was arrived at with the help of profiling live
	// usage.
	debug.SetGCPercent(10)

	// Up some limits.
	if err := limits.SetLimits(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to set limits: %v\n", err)
		os.Exit(1)
	}

	// Call serviceMain on Windows to handle running as a service.  When
	// the return isService flag is true, exit now since we ran as a
	// service.  Otherwise, just fall through to normal operation.
	if runtime.GOOS == "windows" {
		isService, err := winServiceMain()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if isService {
			os.Exit(0)
		}
	}

	// Work around defer not working after os.Exit()
	if err := btcdMain(nil); err != nil {
		os.Exit(1)
	}
}
