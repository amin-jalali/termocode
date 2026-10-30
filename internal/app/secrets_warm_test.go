package app

import "testing"

// main opens the secret store in the background while the terminal is
// probed and the UI is built; the AI state then gets the same store
// without opening it again.
func TestWarmSecretStoreOpensTheStoreAhead(t *testing.T) {
	withConfigDir(t)
	secretStoreConfig.Lock()
	secretStoreConfig.key, secretStoreConfig.store, secretStoreConfig.err = "", nil, nil
	secretStoreConfig.Unlock()

	WarmSecretStore()()
	secretStoreConfig.Lock()
	warm := secretStoreConfig.store
	secretStoreConfig.Unlock()
	if warm == nil {
		t.Fatal("WarmSecretStore did not open the store")
	}
	if got, _ := configureSecretStore(); got != warm {
		t.Fatal("configureSecretStore opened the store again after the warm-up")
	}
}
