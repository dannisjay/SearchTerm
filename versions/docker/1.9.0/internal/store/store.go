package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.etcd.io/bbolt"

	"searchterm/internal/model"
)

var (
	bucketSettings = []byte("settings")
	bucketSites    = []byte("sites")
	bucketTGUsers  = []byte("tg_users")
	bucketTGBots   = []byte("tg_bots")
)

type Store struct {
	db     *bbolt.DB
	aesKey []byte
}

func Open(path, secretKeyHex string) (*Store, error) {
	key, err := hex.DecodeString(secretKeyHex)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("secret key must be 32 bytes hex")
	}
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, aesKey: key}
	err = db.Update(func(tx *bbolt.Tx) error {
		for _, b := range [][]byte{bucketSettings, bucketSites, bucketTGUsers, bucketTGBots} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	gcm, err := s.newGCM()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return hex.EncodeToString(out), nil
}

func (s *Store) Decrypt(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	data, err := hex.DecodeString(enc)
	if err != nil {
		return "", err
	}
	gcm, err := s.newGCM()
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("bad ciphertext")
	}
	nonce, body := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	out, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (s *Store) newGCM() (cipher.AEAD, error) {
	block, err := aes.NewCipher(s.aesKey)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func get[T any](s *Store, bucket []byte, id string, out *T) (bool, error) {
	var found bool
	err := s.db.View(func(tx *bbolt.Tx) error {
		v := tx.Bucket(bucket).Get([]byte(id))
		if v == nil {
			return nil
		}
		found = true
		return json.Unmarshal(v, out)
	})
	return found, err
}

func put(s *Store, bucket []byte, id string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucket).Put([]byte(id), data)
	})
}

func list[T any](s *Store, bucket []byte) ([]T, error) {
	var out []T
	err := s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucket).ForEach(func(_, v []byte) error {
			var item T
			if err := json.Unmarshal(v, &item); err != nil {
				return err
			}
			out = append(out, item)
			return nil
		})
	})
	return out, err
}

func del(s *Store, bucket []byte, id string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucket).Delete([]byte(id))
	})
}

// Settings

func (s *Store) GetSettings() (model.Settings, error) {
	var st model.Settings
	_, err := get(s, bucketSettings, "global", &st)
	return st, err
}

func (s *Store) SaveSettings(st model.Settings) error {
	return put(s, bucketSettings, "global", st)
}

// Sites

func (s *Store) ListSites() ([]model.Site, error) { return list[model.Site](s, bucketSites) }

func (s *Store) GetSite(id string) (model.Site, bool, error) {
	var site model.Site
	found, err := get(s, bucketSites, id, &site)
	return site, found, err
}

func (s *Store) SaveSite(site model.Site) error { return put(s, bucketSites, site.ID, site) }

func (s *Store) DeleteSite(id string) error { return del(s, bucketSites, id) }

// TG users

func (s *Store) ListTGUsers() ([]model.TGUser, error) { return list[model.TGUser](s, bucketTGUsers) }

func (s *Store) SaveTGUser(u model.TGUser) error { return put(s, bucketTGUsers, u.ID, u) }

func (s *Store) DeleteTGUser(id string) error { return del(s, bucketTGUsers, id) }

// TG bots

func (s *Store) ListTGBots() ([]model.TGBot, error) { return list[model.TGBot](s, bucketTGBots) }

func (s *Store) GetTGBot(id string) (model.TGBot, bool, error) {
	var b model.TGBot
	found, err := get(s, bucketTGBots, id, &b)
	return b, found, err
}

func (s *Store) SaveTGBot(b model.TGBot) error { return put(s, bucketTGBots, b.ID, b) }

func (s *Store) DeleteTGBot(id string) error { return del(s, bucketTGBots, id) }
