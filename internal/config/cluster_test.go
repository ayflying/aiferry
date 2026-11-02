package config

import (
	"strings"
	"testing"
)

func TestClusterCallbackAndMasterIdentity(t *testing.T) {
	t.Setenv("AIFERRY_ROLE", "worker")
	t.Setenv("AIFERRY_NODE_NAME", "edge-a")
	t.Setenv("AIFERRY_MASTER_URL", "http://control:8080")
	t.Setenv("AIFERRY_CLUSTER_SECRET", strings.Repeat("s", 32))
	t.Setenv("AIFERRY_NODE_URL", "http://edge:8080/")
	t.Setenv("AIFERRY_MASTER_NODE_NAME", "")
	c, _, err := LoadCluster()
	if err != nil {
		t.Fatal(err)
	}
	if c.NodeURL != "http://edge:8080" || c.MasterNodeName != "master" {
		t.Fatalf("unexpected callback/identity: %+v", c)
	}
	t.Setenv("AIFERRY_MASTER_NODE_NAME", "control-a")
	c, _, err = LoadCluster()
	if err != nil || c.MasterNodeName != "control-a" {
		t.Fatalf("identity: %s %v", c.MasterNodeName, err)
	}
	for _, raw := range []string{"ftp://edge", "http://user:pass@edge", "http://edge?x=1", "http://edge#x", "edge"} {
		t.Setenv("AIFERRY_NODE_URL", raw)
		if _, _, err = LoadCluster(); err == nil {
			t.Fatalf("accepted callback %q", raw)
		}
	}
}
