package config

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/ports"
)

const (
	defaultNodePort      = "5000"
	defaultTimeoutMillis = 3000
	defaultDatabaseName  = "inventory"
	defaultResourceTable = "resources"
)

// Config contiene la configuración necesaria para ensamblar un nodo.
type Config struct {
	LocalNode       model.Node
	Peers           []model.Node
	ListenAddress   string
	ElectionTimeout time.Duration
	DatabaseURI     string
	DatabaseName    string
	ResourceTable   string
}

// StaticMembership implementa la membresía desde configuración estática.
type StaticMembership struct {
	local model.Node
	peers []model.Node
}

// LocalNode devuelve una copia del nodo local.
func (m *StaticMembership) LocalNode() model.Node {
	return m.local
}

// Peers devuelve una copia de la lista de peers para evitar que el dominio la
// modifique accidentalmente.
func (m *StaticMembership) Peers() []model.Node {
	return append([]model.Node(nil), m.peers...)
}

// NewStaticMembership crea la implementación de membresía utilizada por el
// proceso ejecutable.
func NewStaticMembership(local model.Node, peers []model.Node) (*StaticMembership, error) {
	if err := local.Validate(); err != nil {
		return nil, fmt.Errorf("invalid local node: %w", err)
	}

	seen := make(map[model.NodeID]struct{}, len(peers))
	for _, peer := range peers {
		if err := peer.Validate(); err != nil {
			return nil, fmt.Errorf("invalid peer: %w", err)
		}
		if peer.SameIdentity(local) {
			return nil, fmt.Errorf("local node %d cannot also be a peer", local.ID)
		}
		if _, exists := seen[peer.ID]; exists {
			return nil, fmt.Errorf("duplicate peer node id: %d", peer.ID)
		}
		seen[peer.ID] = struct{}{}
	}

	return &StaticMembership{
		local: local,
		peers: append([]model.Node(nil), peers...),
	}, nil
}

var _ ports.MembershipPort = (*StaticMembership)(nil)

// LoadFromEnv carga las variables de entorno del nodo. PEERS utiliza el
// formato "<id>@<host>:<port>,<id>@<host>:<port>" para que la prioridad Bully
// no dependa del orden textual de la configuración.
func LoadFromEnv() (Config, error) {
	nodeID, err := parsePositiveUint("NODE_ID", os.Getenv("NODE_ID"))
	if err != nil {
		return Config{}, err
	}

	host := strings.TrimSpace(os.Getenv("NODE_HOST"))
	if host == "" {
		return Config{}, errors.New("NODE_HOST must not be empty")
	}

	port := strings.TrimSpace(os.Getenv("NODE_PORT"))
	if port == "" {
		port = defaultNodePort
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return Config{}, fmt.Errorf("NODE_PORT must be a valid port: %w", err)
	}

	localAddress := strings.TrimSpace(os.Getenv("LISTEN_ADDRESS"))
	if localAddress == "" {
		localAddress = net.JoinHostPort(host, port)
	}

	peers, err := parsePeers(os.Getenv("PEERS"))
	if err != nil {
		return Config{}, err
	}

	timeoutMillis := defaultTimeoutMillis
	if raw := strings.TrimSpace(os.Getenv("TIMEOUT_MS")); raw != "" {
		timeoutMillis, err = strconv.Atoi(raw)
		if err != nil || timeoutMillis <= 0 {
			return Config{}, errors.New("TIMEOUT_MS must be a positive integer")
		}
	}

	databaseURI := strings.TrimSpace(os.Getenv("DB_URI"))
	if databaseURI == "" {
		return Config{}, errors.New("DB_URI must not be empty")
	}

	databaseName := valueOrDefault("DB_NAME", defaultDatabaseName)
	resourceTable := valueOrDefault("DB_COLLECTION", defaultResourceTable)
	if databaseName == "" || resourceTable == "" {
		return Config{}, errors.New("DB_NAME and DB_COLLECTION must not be empty")
	}

	local := model.Node{ID: model.NodeID(nodeID), Address: localAddress}
	membership, err := NewStaticMembership(local, peers)
	if err != nil {
		return Config{}, err
	}

	return Config{
		LocalNode:       membership.LocalNode(),
		Peers:           membership.Peers(),
		ListenAddress:   localAddress,
		ElectionTimeout: time.Duration(timeoutMillis) * time.Millisecond,
		DatabaseURI:     databaseURI,
		DatabaseName:    databaseName,
		ResourceTable:   resourceTable,
	}, nil
}

// LoadDotEnvFile carga un archivo .env sin sobrescribir variables ya definidas.
// Se mantiene aquí para que el binario pueda usarse con el flujo documentado
// de copiar .env.example a .env sin agregar otra dependencia.
func LoadDotEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return fmt.Errorf("invalid .env entry on line %d", lineNumber)
		}
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("set environment variable %s: %w", key, err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	return nil
}

func parsePeers(raw string) ([]model.Node, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	entries := strings.Split(raw, ",")
	peers := make([]model.Node, 0, len(entries))
	seen := make(map[model.NodeID]struct{}, len(entries))
	for index, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return nil, fmt.Errorf("PEERS contains an empty entry at position %d", index+1)
		}

		idPart, address, found := strings.Cut(entry, "@")
		if !found {
			return nil, fmt.Errorf("PEERS entry %q must use <id>@<address>", entry)
		}
		id, err := parsePositiveUint("PEERS node id", strings.TrimSpace(idPart))
		if err != nil {
			return nil, err
		}
		address = strings.TrimSpace(address)
		if address == "" {
			return nil, fmt.Errorf("PEERS entry %q has an empty address", entry)
		}

		node := model.Node{ID: model.NodeID(id), Address: address}
		if _, exists := seen[node.ID]; exists {
			return nil, fmt.Errorf("duplicate peer node id: %d", node.ID)
		}
		seen[node.ID] = struct{}{}
		peers = append(peers, node)
	}

	return peers, nil
}

func parsePositiveUint(name, raw string) (uint64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, fmt.Errorf("%s must not be empty", name)
	}

	value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil || value == 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}

	return value, nil
}

func valueOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}

	return fallback
}
