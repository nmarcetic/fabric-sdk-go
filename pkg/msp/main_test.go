/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package msp

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/core"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/fab"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/msp"
	"github.com/hyperledger/fabric-sdk-go/pkg/context"
	"github.com/hyperledger/fabric-sdk-go/pkg/core/config/lookup"
	"github.com/hyperledger/fabric-sdk-go/pkg/core/cryptosuite"
	"github.com/hyperledger/fabric-sdk-go/pkg/core/cryptosuite/bccsp/sw"
	"github.com/hyperledger/fabric-sdk-go/pkg/core/mocks"
	fabImpl "github.com/hyperledger/fabric-sdk-go/pkg/fab"
	kvs "github.com/hyperledger/fabric-sdk-go/pkg/fab/keyvaluestore"
	mspapi "github.com/hyperledger/fabric-sdk-go/pkg/msp/api"
	"github.com/hyperledger/fabric-sdk-go/pkg/msp/test/mockmsp"
)

const (
	org1               = "Org1"
	caServerURLListen  = "http://127.0.0.1:0"
	dummyUserStorePath = "/tmp/userstore"
	configPath         = "../core/config/testdata/config_test.yaml"
)

var caServerURL string

type textFixture struct {
	endpointConfig          fab.EndpointConfig
	identityConfig          msp.IdentityConfig
	cryptSuiteConfig        core.CryptoSuiteConfig
	cryptoSuite             core.CryptoSuite
	userStore               msp.UserStore
	caClient                mspapi.CAClient
	identityManagerProvider msp.IdentityManagerProvider
}

var caServer = &mockmsp.MockFabricCAServer{}

func (f *textFixture) setup(configBackend *mocks.MockConfigBackend) { //nolint

	if configBackend == nil {
		backend, err := getCustomBackend(configPath)
		if err != nil {
			panic(err)
		}
		configBackend = backend
	}

	var lis net.Listener
	var err error
	if !caServer.Running() {
		lis, err = net.Listen("tcp", strings.TrimPrefix(caServerURLListen, "http://"))
		if err != nil {
			panic(fmt.Sprintf("Error starting CA Server %s", err))
		}

		caServerURL = "http://" + lis.Addr().String()
	}

	updateCAServerURL(caServerURL, configBackend)

	f.cryptSuiteConfig = cryptosuite.ConfigFromBackend(configBackend)

	f.endpointConfig, err = fabImpl.ConfigFromBackend(configBackend)
	if err != nil {
		panic(fmt.Sprintf("Failed to read config : %v", err))
	}

	f.identityConfig, err = ConfigFromBackend(configBackend)
	if err != nil {
		panic(fmt.Sprintf("Failed to read config : %v", err))
	}

	// Delete all private keys from the crypto suite store
	// and users from the user store
	cleanup(f.cryptSuiteConfig.KeyStorePath())
	cleanup(f.identityConfig.CredentialStorePath())

	f.cryptoSuite, err = sw.GetSuiteByConfig(f.cryptSuiteConfig)
	if f.cryptoSuite == nil {
		panic(fmt.Sprintf("Failed initialize cryptoSuite: %v", err))
	}

	if f.identityConfig.CredentialStorePath() != "" {
		f.userStore, err = NewCertFileUserStore(f.identityConfig.CredentialStorePath())
		if err != nil {
			panic(fmt.Sprintf("creating a user store failed: %v", err))
		}
	}
	f.userStore = userStoreFromConfig(nil, f.identityConfig)

	identityManagers := make(map[string]msp.IdentityManager)
	netConfig, err := f.endpointConfig.NetworkConfig()
	if err != nil {
		panic(fmt.Sprintf("failed to get network config: %v", err))
	}
	for orgName := range netConfig.Organizations {
		mgr, err1 := NewIdentityManager(orgName, f.userStore, f.cryptoSuite, f.endpointConfig)
		if err1 != nil {
			panic(fmt.Sprintf("failed to initialize identity manager for organization: %s, cause :%v", orgName, err1))
		}
		identityManagers[orgName] = mgr
	}

	f.identityManagerProvider = &identityManagerProvider{identityManager: identityManagers}

	ctxProvider := context.NewProvider(context.WithIdentityManagerProvider(f.identityManagerProvider),
		context.WithUserStore(f.userStore), context.WithCryptoSuite(f.cryptoSuite),
		context.WithCryptoSuiteConfig(f.cryptSuiteConfig), context.WithEndpointConfig(f.endpointConfig),
		context.WithIdentityConfig(f.identityConfig))

	ctx := &context.Client{Providers: ctxProvider}

	if err != nil {
		panic(fmt.Sprintf("failed to created context for test setup: %v", err))
	}

	f.caClient, err = NewCAClient(org1, ctx)
	if err != nil {
		panic(fmt.Sprintf("NewCAClient returned error: %v", err))
	}

	// Start Http Server if it's not running
	if !caServer.Running() {
		caServer.Start(lis, f.cryptoSuite)
	}
}

func (f *textFixture) close() {
	cleanup(f.identityConfig.CredentialStorePath())
	cleanup(f.cryptSuiteConfig.KeyStorePath())
}

// readCert Reads a random cert for testing
func readCert(t *testing.T) []byte {
	cert, err := ioutil.ReadFile("testdata/root.pem")
	if err != nil {
		t.Fatalf("Error reading cert: %s", err.Error())
	}
	return cert
}

func cleanup(storePath string) {
	err := os.RemoveAll(storePath)
	if err != nil {
		panic(fmt.Sprintf("Failed to remove dir %s: %v\n", storePath, err))
	}
}

func cleanupTestPath(t *testing.T, storePath string) {
	err := os.RemoveAll(storePath)
	if err != nil {
		t.Fatalf("Cleaning up directory '%s' failed: %v", storePath, err)
	}
}

func mspIDByOrgName(t *testing.T, c fab.EndpointConfig, orgName string) string {
	netConfig, err := c.NetworkConfig()
	if err != nil {
		t.Fatalf("network config retrieval failed: %v", err)
	}

	// viper keys are case insensitive
	orgConfig, ok := netConfig.Organizations[strings.ToLower(orgName)]
	if !ok {
		t.Fatalf("org config retrieval failed: %v", err)
	}
	return orgConfig.MSPID
}

func userStoreFromConfig(t *testing.T, config msp.IdentityConfig) msp.UserStore {
	stateStore, err := kvs.New(&kvs.FileKeyValueStoreOptions{Path: config.CredentialStorePath()})
	if err != nil {
		t.Fatalf("CreateNewFileKeyValueStore failed: %v", err)
	}
	userStore, err := NewCertFileUserStore1(stateStore)
	if err != nil {
		t.Fatalf("CreateNewFileKeyValueStore failed: %v", err)
	}
	return userStore
}

type identityManagerProvider struct {
	identityManager map[string]msp.IdentityManager
}

// IdentityManager returns the organization's identity manager
func (p *identityManagerProvider) IdentityManager(orgName string) (msp.IdentityManager, bool) {
	im, ok := p.identityManager[strings.ToLower(orgName)]
	if !ok {
		return nil, false
	}
	return im, true
}

func updateCAServerURL(caServerURL string, backend *mocks.MockConfigBackend) {

	//get existing certificateAuthorities
	networkConfig := fab.NetworkConfig{}
	lookup.New(backend).UnmarshalKey("certificateAuthorities", &networkConfig.CertificateAuthorities)

	//update URLs
	ca1Config := networkConfig.CertificateAuthorities["ca.org1.example.com"]
	ca1Config.URL = caServerURL

	ca2Config := networkConfig.CertificateAuthorities["ca.org2.example.com"]
	ca2Config.URL = caServerURL

	networkConfig.CertificateAuthorities["ca.org1.example.com"] = ca1Config
	networkConfig.CertificateAuthorities[".ca.org2.example.com"] = ca2Config

	//update backend
	backend.KeyValueMap["certificateAuthorities"] = networkConfig.CertificateAuthorities
}
