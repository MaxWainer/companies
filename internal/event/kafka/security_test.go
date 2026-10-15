package kafka

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"example.com/companies/internal/platform/config"
)

func writeCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	return path
}

func TestSecurityPlaintextByDefault(t *testing.T) {
	s, err := newSecurity(config.Kafka{})
	require.NoError(t, err)
	assert.Nil(t, s.tls)
	assert.Nil(t, s.sasl)
}

func TestSecurityTLSAndSASL(t *testing.T) {
	for mech, name := range map[string]string{
		config.SASLPlain: "PLAIN", config.SASLScramSHA256: "SCRAM-SHA-256", config.SASLScramSHA512: "SCRAM-SHA-512",
	} {
		s, err := newSecurity(config.Kafka{
			TLSEnabled: true, TLSCAFile: writeCA(t),
			SASLMechanism: mech, SASLUsername: "svc", SASLPassword: "secret",
		})
		require.NoError(t, err, mech)
		require.NotNil(t, s.tls)
		assert.Equal(t, uint16(tls.VersionTLS12), s.tls.MinVersion)
		assert.NotNil(t, s.tls.RootCAs, "the extra CA is trusted")
		assert.Equal(t, name, s.sasl.Name())

		tr, d := s.transport(), s.dialer()
		assert.Same(t, s.tls, tr.TLS)
		assert.Equal(t, s.sasl, tr.SASL)
		assert.Same(t, s.tls, d.TLS)
		assert.Equal(t, s.sasl, d.SASLMechanism)
	}
}

func TestSecurityRejectsBadCAFile(t *testing.T) {
	_, err := newSecurity(config.Kafka{TLSEnabled: true, TLSCAFile: filepath.Join(t.TempDir(), "missing.pem")})
	require.ErrorContains(t, err, "read CA file")

	junk := filepath.Join(t.TempDir(), "junk.pem")
	require.NoError(t, os.WriteFile(junk, []byte("not a certificate"), 0o600))
	_, err = newSecurity(config.Kafka{TLSEnabled: true, TLSCAFile: junk})
	require.ErrorContains(t, err, "no certificates")
}
