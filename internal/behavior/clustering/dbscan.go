package clustering

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/saaga0h/jeeves-platform/internal/behavior/storage"
)

// DBSCANConfig configures the DBSCAN clustering algorithm
type DBSCANConfig struct {
	Epsilon   float64 // maximum distance for neighborhood (default: 0.3)
	MinPoints int     // minimum points to form cluster (default: 5)
}

// Cluster represents a group of semantically similar anchors
type Cluster struct {
	ID      int
	Members []uuid.UUID // anchor IDs
	Noise   bool        // true if this is noise cluster
}

// ClusteringEngine performs DBSCAN clustering on semantic anchors
type ClusteringEngine struct {
	config  DBSCANConfig
	storage *storage.AnchorStorage
	logger  *slog.Logger
}

// NewClusteringEngine creates a new clustering engine
func NewClusteringEngine(
	config DBSCANConfig,
	storage *storage.AnchorStorage,
	logger *slog.Logger,
) *ClusteringEngine {
	return &ClusteringEngine{
		config:  config,
		storage: storage,
		logger:  logger,
	}
}

// ClusterAnchors performs DBSCAN clustering on anchor set
func (e *ClusteringEngine) ClusterAnchors(
	ctx context.Context,
	anchorIDs []uuid.UUID,
) ([]*Cluster, error) {

	if len(anchorIDs) < e.config.MinPoints {
		return nil, fmt.Errorf("insufficient anchors for clustering: %d < %d",
			len(anchorIDs), e.config.MinPoints)
	}

	e.logger.Info("Starting DBSCAN clustering",
		"anchors", len(anchorIDs),
		"epsilon", e.config.Epsilon,
		"min_points", e.config.MinPoints)

	// Load distance matrix
	distances, err := e.loadDistanceMatrix(ctx, anchorIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to load distances: %w", err)
	}

	e.logger.Debug("Loaded distance matrix", "pairs", len(distances))

	// Run DBSCAN
	clusters := e.dbscan(anchorIDs, distances)

	// Count noise points
	noiseCount := 0
	validClusters := 0
	for _, cluster := range clusters {
		if cluster.Noise {
			noiseCount = len(cluster.Members)
		} else {
			validClusters++
		}
	}

	e.logger.Info("Clustering completed",
		"clusters_found", validClusters,
		"noise_points", noiseCount)

	return clusters, nil
}

func (e *ClusteringEngine) loadDistanceMatrix(
	ctx context.Context,
	anchorIDs []uuid.UUID,
) (map[string]float64, error) {

	distances := make(map[string]float64)

	for i := 0; i < len(anchorIDs); i++ {
		for j := i + 1; j < len(anchorIDs); j++ {
			dist, err := e.storage.GetDistance(ctx, anchorIDs[i], anchorIDs[j])
			if err != nil || dist == nil {
				// Missing distance - skip this pair
				e.logger.Debug("Missing distance",
					"anchor1", anchorIDs[i],
					"anchor2", anchorIDs[j],
					"error", err)
				continue
			}

			key := distanceKey(anchorIDs[i], anchorIDs[j])
			distances[key] = dist.Distance
		}
	}

	return distances, nil
}

// distanceKey creates a canonical key for distance lookup
func distanceKey(id1, id2 uuid.UUID) string {
	// Ensure consistent ordering
	if id1.String() < id2.String() {
		return id1.String() + "-" + id2.String()
	}
	return id2.String() + "-" + id1.String()
}

// dbscan implements the DBSCAN clustering algorithm
func (e *ClusteringEngine) dbscan(
	anchorIDs []uuid.UUID,
	distances map[string]float64,
) []*Cluster {

	// Track visited and cluster assignments
	visited := make(map[uuid.UUID]bool)
	clusterID := make(map[uuid.UUID]int)

	currentCluster := 0

	for _, anchorID := range anchorIDs {
		if visited[anchorID] {
			continue
		}

		visited[anchorID] = true

		// Get neighbors within epsilon
		neighbors := e.getNeighbors(anchorID, anchorIDs, distances)

		if len(neighbors) < e.config.MinPoints {
			// Mark as noise (will be cluster -1)
			clusterID[anchorID] = -1
			e.logger.Debug("Marked as noise (insufficient neighbors)",
				"anchor", anchorID,
				"neighbors", len(neighbors))
			continue
		}

		// Start new cluster
		currentCluster++
		clusterID[anchorID] = currentCluster

		e.logger.Debug("Starting new cluster",
			"cluster_id", currentCluster,
			"seed_anchor", anchorID,
			"neighbors", len(neighbors))

		// Expand cluster
		e.expandCluster(anchorID, neighbors, currentCluster, anchorIDs, distances, visited, clusterID)
	}

	// Build cluster objects
	clusterMap := make(map[int]*Cluster)

	for anchorID, cid := range clusterID {
		if _, exists := clusterMap[cid]; !exists {
			clusterMap[cid] = &Cluster{
				ID:      cid,
				Members: []uuid.UUID{},
				Noise:   cid == -1,
			}
		}
		clusterMap[cid].Members = append(clusterMap[cid].Members, anchorID)
	}

	// Convert to slice
	var clusters []*Cluster
	for _, cluster := range clusterMap {
		clusters = append(clusters, cluster)
	}

	return clusters
}

func (e *ClusteringEngine) getNeighbors(
	anchorID uuid.UUID,
	allAnchors []uuid.UUID,
	distances map[string]float64,
) []uuid.UUID {

	var neighbors []uuid.UUID

	for _, otherID := range allAnchors {
		if anchorID == otherID {
			continue
		}

		key := distanceKey(anchorID, otherID)
		dist, exists := distances[key]

		if exists && dist <= e.config.Epsilon {
			neighbors = append(neighbors, otherID)
		}
	}

	return neighbors
}

func (e *ClusteringEngine) expandCluster(
	anchorID uuid.UUID,
	neighbors []uuid.UUID,
	clusterNum int,
	allAnchors []uuid.UUID,
	distances map[string]float64,
	visited map[uuid.UUID]bool,
	clusterID map[uuid.UUID]int,
) {

	i := 0
	for i < len(neighbors) {
		neighborID := neighbors[i]

		if !visited[neighborID] {
			visited[neighborID] = true

			// Get neighbors of neighbor
			neighborNeighbors := e.getNeighbors(neighborID, allAnchors, distances)

			if len(neighborNeighbors) >= e.config.MinPoints {
				// Add new neighbors to expansion list
				neighbors = append(neighbors, neighborNeighbors...)
			}
		}

		if cid, exists := clusterID[neighborID]; !exists || cid == -1 {
			// Assign to cluster
			clusterID[neighborID] = clusterNum
		}

		i++
	}
}
