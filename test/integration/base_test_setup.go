/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package integration

import (
	"os"
	"path"

	"fmt"

	"github.com/hyperledger/fabric-sdk-go/pkg/client/resmgmt"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/errors/retry"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/msp"
	"github.com/hyperledger/fabric-sdk-go/pkg/fab"
	packager "github.com/hyperledger/fabric-sdk-go/pkg/fab/ccpackager/gopackager"
	"github.com/hyperledger/fabric-sdk-go/pkg/fabsdk"
	"github.com/hyperledger/fabric-sdk-go/third_party/github.com/hyperledger/fabric/common/cauthdsl"
	"github.com/pkg/errors"
)

// BaseSetupImpl implementation of BaseTestSetup
type BaseSetupImpl struct {
	Identity          msp.Identity
	Targets           []string
	ConfigFile        string
	OrgID             string
	ChannelID         string
	ChannelConfigFile string
}

// Initial B values for ExampleCC
const (
	ExampleCCInitB    = "200"
	ExampleCCUpgradeB = "400"
	AdminUser         = "Admin"
)

// ExampleCC query and transaction arguments
var queryArgs = [][]byte{[]byte("query"), []byte("b")}
var txArgs = [][]byte{[]byte("move"), []byte("a"), []byte("b"), []byte("1")}

// ExampleCC init and upgrade args
var initArgs = [][]byte{[]byte("init"), []byte("a"), []byte("100"), []byte("b"), []byte(ExampleCCInitB)}
var upgradeArgs = [][]byte{[]byte("init"), []byte("a"), []byte("100"), []byte("b"), []byte(ExampleCCUpgradeB)}

// ExampleCCQueryArgs returns example cc query args
func ExampleCCQueryArgs() [][]byte {
	return queryArgs
}

// ExampleCCTxArgs returns example cc move funds args
func ExampleCCTxArgs() [][]byte {
	return txArgs
}

//ExampleCCInitArgs returns example cc initialization args
func ExampleCCInitArgs() [][]byte {
	return initArgs
}

//ExampleCCUpgradeArgs returns example cc upgrade args
func ExampleCCUpgradeArgs() [][]byte {
	return upgradeArgs
}

// Initialize reads configuration from file and sets up client, channel and event hub
func (setup *BaseSetupImpl) Initialize(sdk *fabsdk.FabricSDK) error {

	adminIdentity, err := GetSigningIdentity(sdk, AdminUser, setup.OrgID)
	if err != nil {
		return errors.WithMessage(err, "failed to get client context")
	}
	setup.Identity = adminIdentity

	configBackend, err := sdk.Config()
	if err != nil {
		//For some tests SDK may not have backend set, try with config file if backend is missing
		configBackend, err = ConfigBackend()
		if err != nil {
			return errors.Wrapf(err, "failed to get config backend from config: %v", err)
		}
	}

	targets, err := OrgTargetPeers(configBackend, []string{setup.OrgID})
	if err != nil {
		return errors.Wrapf(err, "loading target peers from config failed")
	}
	setup.Targets = targets

	r, err := os.Open(setup.ChannelConfigFile)
	if err != nil {
		return errors.Wrapf(err, "opening channel config file failed")
	}
	defer func() {
		if err = r.Close(); err != nil {
			fmt.Printf("close error %v\n", err)
		}

	}()

	// Create channel for tests
	req := resmgmt.SaveChannelRequest{ChannelID: setup.ChannelID, ChannelConfig: r, SigningIdentities: []msp.SigningIdentity{adminIdentity}}
	if err = InitializeChannel(sdk, setup.OrgID, req, targets); err != nil {
		return errors.WithMessage(err, "failed to initialize channel")
	}

	return nil
}

// GetDeployPath ..
func GetDeployPath() string {
	pwd, _ := os.Getwd()
	return path.Join(pwd, "../../fixtures/testdata")
}

// InstallAndInstantiateExampleCC install and instantiate using resource management client
func InstallAndInstantiateExampleCC(sdk *fabsdk.FabricSDK, user fabsdk.ContextOption, orgName string, chainCodeID string) (resmgmt.InstantiateCCResponse, error) {
	return InstallAndInstantiateCC(sdk, user, orgName, chainCodeID, "github.com/example_cc", "v0", GetDeployPath(), initArgs)
}

// InstallAndInstantiateCC install and instantiate using resource management client
func InstallAndInstantiateCC(sdk *fabsdk.FabricSDK, user fabsdk.ContextOption, orgName string, ccName, ccPath, ccVersion, goPath string, ccArgs [][]byte) (resmgmt.InstantiateCCResponse, error) {

	ccPkg, err := packager.NewCCPackage(ccPath, goPath)
	if err != nil {
		return resmgmt.InstantiateCCResponse{}, errors.WithMessage(err, "creating chaincode package failed")
	}

	configBackend, err := sdk.Config()
	if err != nil {
		return resmgmt.InstantiateCCResponse{}, errors.WithMessage(err, "failed to get config backend")
	}

	endpointConfig, err := fab.ConfigFromBackend(configBackend)
	if err != nil {
		return resmgmt.InstantiateCCResponse{}, errors.WithMessage(err, "failed to get endpoint config")
	}

	mspID, err := endpointConfig.MSPID(orgName)
	if err != nil {
		return resmgmt.InstantiateCCResponse{}, errors.WithMessage(err, "looking up MSP ID failed")
	}

	//prepare context
	clientContext := sdk.Context(user, fabsdk.WithOrg(orgName))

	// Resource management client is responsible for managing resources (joining channels, install/instantiate/upgrade chaincodes)
	resMgmtClient, err := resmgmt.New(clientContext)
	if err != nil {
		return resmgmt.InstantiateCCResponse{}, errors.WithMessage(err, "Failed to create new resource management client")
	}

	_, err = resMgmtClient.InstallCC(resmgmt.InstallCCRequest{Name: ccName, Path: ccPath, Version: ccVersion, Package: ccPkg}, resmgmt.WithRetry(retry.DefaultResMgmtOpts))
	if err != nil {
		return resmgmt.InstantiateCCResponse{}, err
	}

	ccPolicy := cauthdsl.SignedByMspMember(mspID)
	return resMgmtClient.InstantiateCC("mychannel", resmgmt.InstantiateCCRequest{Name: ccName, Path: ccPath, Version: ccVersion, Args: ccArgs, Policy: ccPolicy}, resmgmt.WithRetry(retry.DefaultResMgmtOpts))
}

// GetSigningIdentity returns signing identity
//TODO : not a recommended way to get idenity, will be replaced
func GetSigningIdentity(sdk *fabsdk.FabricSDK, user, orgID string) (msp.SigningIdentity, error) {
	idenityContext := sdk.Context(fabsdk.WithUser(user), fabsdk.WithOrg(orgID))
	return idenityContext()
}
