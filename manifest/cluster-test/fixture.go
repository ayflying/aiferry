// Standalone, standard-library-only SQL fixture emitter. Never import app startup.
package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cluster-test fixture:", err)
		os.Exit(1)
	}
}

func run() error {
	if os.Getenv("AIFERRY_ROLE") != "master" || os.Getenv("MYSQL_DATABASE") != "aiferry_cluster_test" {
		return fmt.Errorf("refusing non-test database or non-master role")
	}
	var input struct {
		APIKey      string `json:"api_key"`
		UpstreamKey string `json:"upstream_key"`
	}
	if err := json.NewDecoder(io.LimitReader(os.Stdin, 4096)).Decode(&input); err != nil {
		return err
	}
	if !strings.HasPrefix(input.APIKey, "sk-cluster-test-") || len(input.UpstreamKey) < 32 {
		return fmt.Errorf("invalid test input")
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("AIFERRY_MASTER_KEY"))
	if err != nil || len(key) != 32 {
		return fmt.Errorf("invalid test master key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	encrypt := func(value string) (string, error) {
		nonce := make([]byte, aead.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return "", err
		}
		return base64.RawStdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(value), nil)), nil
	}
	upstream, err := encrypt(input.UpstreamKey)
	if err != nil {
		return err
	}
	apiCipher, err := encrypt(input.APIKey)
	if err != nil {
		return err
	}
	hash := func(value string) string {
		digest := sha256.Sum256([]byte(value))
		return hex.EncodeToString(digest[:])
	}
	// Every interpolated value is generated base64/hex or a quoted test prefix.
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	fmt.Printf(`START TRANSACTION;
INSERT INTO users (id,name,email,role,status,balance) VALUES (990001,'Cluster test','cluster-test@example.invalid','user',1,100)
 ON DUPLICATE KEY UPDATE status=1,deleted_at=NULL;
INSERT INTO api_keys (id,user_id,name,key_prefix,key_hash,key_cipher,status,spend_limit) VALUES (990001,990001,'Cluster test',%s,%s,%s,1,100)
 ON DUPLICATE KEY UPDATE key_hash=VALUES(key_hash),key_cipher=VALUES(key_cipher),status=1,deleted_at=NULL;
INSERT INTO channels (id,created_by_user_id,name,type,base_url,api_key_cipher,status,priority,weight,auto_disable_enabled) VALUES (990001,990001,'Cluster mock','openai','http://mock:8090',%s,1,100,1,0)
 ON DUPLICATE KEY UPDATE created_by_user_id=990001,api_key_cipher=VALUES(api_key_cipher),base_url=VALUES(base_url),status=1,deleted_at=NULL;
INSERT INTO channel_credentials (id,channel_id,key_prefix,key_hash,api_key_cipher,status) VALUES (990001,990001,'cluster-test',%s,%s,1)
 ON DUPLICATE KEY UPDATE key_hash=VALUES(key_hash),api_key_cipher=VALUES(api_key_cipher),status=1,deleted_at=NULL;
INSERT INTO channel_models (id,channel_id,public_name,upstream_name,discovered,enabled,input_price,cached_input_price,output_price) VALUES (990001,990001,'cluster-test-model','mock-gpt',0,1,1,0.5,2)
 ON DUPLICATE KEY UPDATE enabled=1,deleted_at=NULL;
INSERT INTO model_prices (public_name,billing_mode,input_price,cached_input_price,output_price) VALUES ('cluster-test-model','token',1,0.5,2)
 ON DUPLICATE KEY UPDATE billing_mode='token',input_price=1,cached_input_price=0.5,output_price=2,deleted_at=NULL;
INSERT IGNORE INTO api_key_models (api_key_id,model_name) VALUES (990001,'cluster-test-model');
UPDATE cluster_state SET config_version=config_version+1 WHERE id=1;
COMMIT;
`, quote(input.APIKey[:12]), quote(hash(input.APIKey)), quote(apiCipher), quote(upstream), quote(hash(input.UpstreamKey)), quote(upstream))
	return nil
}
