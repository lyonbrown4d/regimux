package object

import (
	"context"
	"net"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"github.com/spf13/afero/sftpfs"
	"go.uber.org/multierr"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const defaultSFTPTimeout = 10 * time.Second

type SFTPStore struct {
	*aferoStore
}

var _ Store = (*SFTPStore)(nil)
var _ ObjectWalker = (*SFTPStore)(nil)
var _ ObjectLister = (*SFTPStore)(nil)

func NewSFTP(ctx context.Context, root string, opts SFTPOptions) (*SFTPStore, error) {
	ctx = normalizeContext(ctx)
	if err := checkContext(ctx, "create sftp object store"); err != nil {
		return nil, err
	}
	opts = normalizeSFTPOptions(opts)
	if opts.Addr == "" {
		return nil, errorf("sftp object store addr is required")
	}
	if opts.Username == "" {
		return nil, errorf("sftp object store username is required")
	}
	client, sshClient, err := dialSFTP(ctx, opts)
	if err != nil {
		return nil, err
	}
	store, err := newAferoStore(newSlashPathFS(sftpfs.New(client)), sftpBasePath(root), false, true)
	if err != nil {
		return nil, joinError("create sftp object store and close clients", err, closeSFTPClients(client, sshClient))
	}
	store.close = func() error {
		return closeSFTPClients(client, sshClient)
	}
	return &SFTPStore{aferoStore: store}, nil
}

func (s *SFTPStore) WalkObjects(ctx context.Context, fn ObjectWalkFunc) error {
	if s == nil || s.aferoStore == nil {
		return errorf("object store is not configured")
	}
	return s.aferoStore.walkObjects(ctx, fn)
}

func (s *SFTPStore) ListObjects(ctx context.Context) ([]Info, error) {
	if s == nil || s.aferoStore == nil {
		return nil, errorf("object store is not configured")
	}
	return s.aferoStore.listObjects(ctx)
}

func normalizeSFTPOptions(opts SFTPOptions) SFTPOptions {
	opts.Addr = strings.TrimSpace(opts.Addr)
	opts.Username = strings.TrimSpace(opts.Username)
	opts.Password = strings.TrimSpace(opts.Password)
	opts.PrivateKey = strings.TrimSpace(opts.PrivateKey)
	opts.PrivateKeyPassphrase = strings.TrimSpace(opts.PrivateKeyPassphrase)
	opts.KnownHostsPath = strings.TrimSpace(opts.KnownHostsPath)
	opts.HostKey = strings.TrimSpace(opts.HostKey)
	if opts.Timeout == 0 {
		opts.Timeout = defaultSFTPTimeout
	}
	return opts
}

func sftpBasePath(root string) string {
	root = strings.TrimSpace(strings.ReplaceAll(root, "\\", "/"))
	if root == "" {
		return "data/objects"
	}
	return path.Clean(root)
}

func dialSFTP(ctx context.Context, opts SFTPOptions) (*sftp.Client, *ssh.Client, error) {
	sshConfig, err := newSSHClientConfig(opts)
	if err != nil {
		return nil, nil, err
	}
	dialer := net.Dialer{Timeout: opts.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", opts.Addr)
	if err != nil {
		return nil, nil, wrapError(err, "dial sftp object store")
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, opts.Addr, sshConfig)
	if err != nil {
		return nil, nil, joinError("open ssh client connection and close tcp connection", wrapError(err, "open ssh client connection"), closeTCPConn(conn))
	}
	sshClient := ssh.NewClient(sshConn, chans, reqs)
	client, err := sftp.NewClient(sshClient)
	if err != nil {
		return nil, nil, joinError("open sftp client and close ssh client", wrapError(err, "open sftp client"), closeSSHClient(sshClient))
	}
	return client, sshClient, nil
}

func newSSHClientConfig(opts SFTPOptions) (*ssh.ClientConfig, error) {
	auth, err := sftpAuthMethods(opts)
	if err != nil {
		return nil, err
	}
	hostKeyCallback, err := sftpHostKeyCallback(opts)
	if err != nil {
		return nil, err
	}
	return &ssh.ClientConfig{
		User:            opts.Username,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback,
		Timeout:         opts.Timeout,
	}, nil
}

func sftpAuthMethods(opts SFTPOptions) ([]ssh.AuthMethod, error) {
	auth := make([]ssh.AuthMethod, 0, 2)
	if opts.Password != "" {
		auth = append(auth, ssh.Password(opts.Password))
	}
	if opts.PrivateKey != "" {
		signer, err := parseSFTPPrivateKey(opts.PrivateKey, opts.PrivateKeyPassphrase)
		if err != nil {
			return nil, err
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if len(auth) == 0 {
		return nil, errorf("sftp object store password or private_key is required")
	}
	return auth, nil
}

func parseSFTPPrivateKey(privateKey, passphrase string) (ssh.Signer, error) {
	if passphrase == "" {
		signer, err := ssh.ParsePrivateKey([]byte(privateKey))
		if err != nil {
			return nil, wrapError(err, "parse sftp private key")
		}
		return signer, nil
	}
	signer, err := ssh.ParsePrivateKeyWithPassphrase([]byte(privateKey), []byte(passphrase))
	if err != nil {
		return nil, wrapError(err, "parse encrypted sftp private key")
	}
	return signer, nil
}

func sftpHostKeyCallback(opts SFTPOptions) (ssh.HostKeyCallback, error) {
	if opts.HostKey != "" {
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(opts.HostKey))
		if err != nil {
			return nil, wrapError(err, "parse sftp host key")
		}
		return ssh.FixedHostKey(key), nil
	}
	if opts.KnownHostsPath == "" {
		return nil, errorf("sftp object store known_hosts_path or host_key is required")
	}
	callback, err := knownhosts.New(opts.KnownHostsPath)
	if err != nil {
		return nil, wrapError(err, "load sftp known hosts")
	}
	return callback, nil
}

func closeSFTPClients(client *sftp.Client, sshClient *ssh.Client) error {
	var err error
	if client != nil {
		err = multierr.Append(err, client.Close())
	}
	if sshClient != nil {
		err = multierr.Append(err, sshClient.Close())
	}
	if err != nil {
		return wrapError(err, "close sftp clients")
	}
	return nil
}

func closeTCPConn(conn net.Conn) error {
	if conn == nil {
		return nil
	}
	if err := conn.Close(); err != nil {
		return wrapError(err, "close tcp connection")
	}
	return nil
}

func closeSSHClient(client *ssh.Client) error {
	if client == nil {
		return nil
	}
	if err := client.Close(); err != nil {
		return wrapError(err, "close ssh client")
	}
	return nil
}
