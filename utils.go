package main

import (
	"io"
	"io/ioutil"
	"strings"

	"golang.org/x/crypto/ssh"
)

func strip(v string) string {
	return strings.TrimSpace(strings.Trim(v, "\n"))
}

type keychain struct {
	key ssh.Signer
}

func (k *keychain) PublicKey() ssh.PublicKey {
	return k.key.PublicKey()
}

func (k *keychain) Sign(rand io.Reader, data []byte) (sig *ssh.Signature, err error) {
	signature, err := k.key.Sign(rand, data)
	if err != nil {
		return nil, err
	}
	return signature, err
}

func (k *keychain) loadPEM(file string) error {
	buf, err := ioutil.ReadFile(file)
	if err != nil {
		return err

	}
	key, err := ssh.ParsePrivateKey(buf)
	if err != nil {
		return err

	}
	k.key = key
	return nil
}
