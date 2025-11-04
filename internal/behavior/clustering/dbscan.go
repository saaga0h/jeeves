package clustering

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"

	"github.com/saaga0h/jeeves-platform/internal/behavior/storage"
	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
)

// structuredDist computes distance using block-wise metrics for 128D structured tensor
// This is a copy of the function from distance/computation_agent.go to avoid circular imports
func structuredDist(v1, v2 pgvector.Vector) float64 {
	s1 := v1.Slice()
	s2 := v2.Slice()

	// 1. Temporal distance (cyclic, dimensions 0-3)
	temporalDist := cyclicDistance(s1[0:4], s2[0:4])

	// 2. Seasonal distance (cyclic, dimensions 4-7)
	seasonalDist := cyclicDistance(s1[4:8], s2[4:8])

	// 3. Day type distance (categorical, dimensions 8-11)
	dayTypeDist := euclideanDistance(s1[8:12], s2[8:12])

	// 4. Spatial/Location distance (semantic, dimensions 12-27)
	spatialDist := 1.0 - cosineSimilaritySlice(s1[12:28], s2[12:28])

	// 5. Weather distance (continuous, dimensions 28-43)
	weatherDist := euclideanDistance(s1[28:44], s2[28:44])

	// 6. Lighting distance (dimensions 44-59)
	lightingDist := euclideanDistance(s1[44:60], s2[44:60])

	// 7. Activity signals (dimensions 60-79)
	activityDist := euclideanDistance(s1[60:80], s2[60:80])

	// 8. Household rhythm (dimensions 80-95)
	rhythmDist := euclideanDistance(s1[80:96], s2[80:96])

	// Weighted combination
	distance := 0.10*temporalDist +
		0.05*seasonalDist +
		0.10*dayTypeDist +
		0.30*spatialDist +
		0.05*weatherDist +
		0.10*lightingDist +
		0.25*activityDist +
		0.05*rhythmDist

	return math.Max(0, math.Min(1, distance))
}

func cyclicDistance(v1, v2 []float32) float64 {
	var totalDist float64
	pairs := len(v1) / 2

	for i := 0; i < pairs; i++ {
		sin1 := float64(v1[i*2])
		cos1 := float64(v1[i*2+1])
		sin2 := float64(v2[i*2])
		cos2 := float64(v2[i*2+1])

		dotProd := sin1*sin2 + cos1*cos2
		dotProd = math.Max(-1.0, math.Min(1.0, dotProd))

		angle := math.Acos(dotProd)
		totalDist += angle / math.Pi
	}

	return totalDist / float64(pairs)
}

func euclideanDistance(v1, v2 []float32) float64 {
	var sum float64
	for i := 0; i < len(v1); i++ {
		diff := float64(v1[i]) - float64(v2[i])
		sum += diff * diff
	}
	distance := math.Sqrt(sum) / math.Sqrt(2.0)
	return math.Min(1.0, distance)
}

func cosineSimilaritySlice(v1, v2 []float32) float64 {
	var dot, mag1, mag2 float64
	for i := 0; i < len(v1); i++ {
		dot += float64(v1[i]) * float64(v2[i])
		mag1 += float64(v1[i]) * float64(v1[i])
		mag2 += float64(v2[i]) * float64(v2[i])
	}

	if mag1 == 0 || mag2 == 0 {
		return 0
	}

	return dot / (math.Sqrt(mag1) * math.Sqrt(mag2))
}

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
	return e.ClusterAnchorsWithEpsilon(ctx, anchorIDs, e.config.Epsilon)
}

// ClusterAnchorsWithEpsilon performs DBSCAN clustering with custom epsilon
func (e *ClusteringEngine) ClusterAnchorsWithEpsilon(
	ctx context.Context,
	anchorIDs []uuid.UUID,
	epsilon float64,
) ([]*Cluster, error) {

	if len(anchorIDs) < e.config.MinPoints {
		return nil, fmt.Errorf("insufficient anchors for clustering: %d < %d",
			len(anchorIDs), e.config.MinPoints)
	}

	e.logger.Info("Starting DBSCAN clustering",
		"anchors", len(anchorIDs),
		"epsilon", epsilon,
		"min_points", e.config.MinPoints)

	// Load distance matrix
	distances, err := e.loadDistanceMatrix(ctx, anchorIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to load distances: %w", err)
	}

	e.logger.Debug("Loaded distance matrix", "pairs", len(distances))

	// Run DBSCAN with custom epsilon
	clusters := e.dbscanWithEpsilon(anchorIDs, distances, epsilon)

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

	// Load all anchors with embeddings for in-memory distance computation
	anchors, err := e.storage.GetAnchorsByIDs(ctx, anchorIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to load anchors: %w", err)
	}

	// Create anchor lookup map
	anchorMap := make(map[uuid.UUID]*types.SemanticAnchor)
	for i := range anchors {
		anchorMap[anchors[i].ID] = anchors[i]
	}

	// Compute all pairwise distances in-memory using structured distance
	missingCount := 0
	sampleCount := 0
	for i := 0; i < len(anchorIDs); i++ {
		for j := i + 1; j < len(anchorIDs); j++ {
			anchor1, ok1 := anchorMap[anchorIDs[i]]
			anchor2, ok2 := anchorMap[anchorIDs[j]]

			if !ok1 || !ok2 {
				e.logger.Warn("Missing anchor in map",
					"anchor1_found", ok1,
					"anchor2_found", ok2)
				continue
			}

			// Compute structured distance in-memory
			dist := structuredDist(anchor1.SemanticEmbedding, anchor2.SemanticEmbedding)

			// DEBUG: Log sample distances between different locations
			if sampleCount < 10 && anchor1.Location != anchor2.Location {
				e.logger.Info("DEBUG: Cross-location distance",
					"loc1", anchor1.Location,
					"loc2", anchor2.Location,
					"distance", dist)
				sampleCount++
			}

			key := distanceKey(anchorIDs[i], anchorIDs[j])
			distances[key] = dist
			missingCount++
		}
	}

	// Calculate distance statistics
	var minDist, maxDist, sumDist float64
	minDist = 1.0
	maxDist = 0.0
	for _, dist := range distances {
		if dist < minDist {
			minDist = dist
		}
		if dist > maxDist {
			maxDist = dist
		}
		sumDist += dist
	}
	avgDist := sumDist / float64(len(distances))

	// Calculate standard deviation
	var varianceSum float64
	for _, dist := range distances {
		diff := dist - avgDist
		varianceSum += diff * diff
	}
	stdDev := 0.0
	if len(distances) > 0 {
		stdDev = math.Sqrt(varianceSum / float64(len(distances)))
	}

	e.logger.Info("Computed distance matrix in-memory",
		"total_pairs", len(distances),
		"computed_fresh", missingCount,
		"min_distance", minDist,
		"max_distance", maxDist,
		"avg_distance", avgDist,
		"std_dev", stdDev)

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

// dbscan implements the DBSCAN clustering algorithm (uses default epsilon)
func (e *ClusteringEngine) dbscan(
	anchorIDs []uuid.UUID,
	distances map[string]float64,
) []*Cluster {
	return e.dbscanWithEpsilon(anchorIDs, distances, e.config.Epsilon)
}

// dbscanWithEpsilon implements DBSCAN with custom epsilon
func (e *ClusteringEngine) dbscanWithEpsilon(
	anchorIDs []uuid.UUID,
	distances map[string]float64,
	epsilon float64,
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
		neighbors := e.getNeighborsWithEpsilon(anchorID, anchorIDs, distances, epsilon)

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
		e.expandClusterWithEpsilon(anchorID, neighbors, currentCluster, anchorIDs, distances, visited, clusterID, epsilon)
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
	return e.getNeighborsWithEpsilon(anchorID, allAnchors, distances, e.config.Epsilon)
}

func (e *ClusteringEngine) getNeighborsWithEpsilon(
	anchorID uuid.UUID,
	allAnchors []uuid.UUID,
	distances map[string]float64,
	epsilon float64,
) []uuid.UUID {

	var neighbors []uuid.UUID

	for _, otherID := range allAnchors {
		if anchorID == otherID {
			continue
		}

		key := distanceKey(anchorID, otherID)
		dist, exists := distances[key]

		if exists && dist <= epsilon {
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
	e.expandClusterWithEpsilon(anchorID, neighbors, clusterNum, allAnchors, distances, visited, clusterID, e.config.Epsilon)
}

func (e *ClusteringEngine) expandClusterWithEpsilon(
	anchorID uuid.UUID,
	neighbors []uuid.UUID,
	clusterNum int,
	allAnchors []uuid.UUID,
	distances map[string]float64,
	visited map[uuid.UUID]bool,
	clusterID map[uuid.UUID]int,
	epsilon float64,
) {

	i := 0
	for i < len(neighbors) {
		neighborID := neighbors[i]

		if !visited[neighborID] {
			visited[neighborID] = true

			// Get neighbors of neighbor
			neighborNeighbors := e.getNeighborsWithEpsilon(neighborID, allAnchors, distances, epsilon)

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
