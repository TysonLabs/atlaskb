package pipeline

import (
	"github.com/google/uuid"
)

// relWeights maps relationship kinds to edge weights for the community detection graph.
var relWeights = map[string]float64{
	"calls":      3.0,
	"implements": 2.5,
	"depends_on": 2.0,
	"owns":       1.5,
	"imports":    1.0,
}

// Graph is an undirected weighted graph for community detection.
type Graph struct {
	n        int                // node count
	adj      []map[int]float64 // adjacency: adj[u][v] = weight
	idToNode map[uuid.UUID]int
	nodeToID []uuid.UUID
	totalW   float64 // sum of all edge weights (each edge counted once)
}

// NewGraph creates a new empty graph with the given initial capacity.
func NewGraph(capacity int) *Graph {
	return &Graph{
		adj:      make([]map[int]float64, 0, capacity),
		idToNode: make(map[uuid.UUID]int, capacity),
		nodeToID: make([]uuid.UUID, 0, capacity),
	}
}

// AddNode adds a node to the graph if it doesn't already exist, and returns its index.
func (g *Graph) AddNode(id uuid.UUID) int {
	if idx, ok := g.idToNode[id]; ok {
		return idx
	}
	idx := g.n
	g.n++
	g.idToNode[id] = idx
	g.nodeToID = append(g.nodeToID, id)
	g.adj = append(g.adj, make(map[int]float64))
	return idx
}

// AddEdge adds an undirected weighted edge between two nodes.
// If both nodes share the same UUID or either is missing, this is a no-op.
// If the edge already exists, weights are accumulated.
func (g *Graph) AddEdge(from, to uuid.UUID, weight float64) {
	if from == to {
		return
	}
	u, okU := g.idToNode[from]
	v, okV := g.idToNode[to]
	if !okU || !okV {
		return
	}
	g.adj[u][v] += weight
	g.adj[v][u] += weight
	g.totalW += weight
}

// LouvainResult holds the output of community detection.
type LouvainResult struct {
	Communities    map[int]int // node index -> community ID
	NumCommunities int
	Modularity     float64
}

// louvainDetect runs Louvain community detection on a copy of the graph,
// preserving the original graph state. Returns a mapping from original node
// index to community ID.
func (g *Graph) louvainDetect(maxPasses int, minDelta float64) *LouvainResult {
	if g.n == 0 || g.totalW == 0 {
		return &LouvainResult{
			Communities:    make(map[int]int),
			NumCommunities: 0,
			Modularity:     0,
		}
	}

	n := g.n
	// m = sum of all edge weights. In our adjacency list each undirected edge
	// is stored twice (adj[u][v] and adj[v][u]), so the sum of all adj entries
	// equals 2*m. We use m2 = 2*m for the modularity formulas.
	m2 := 0.0
	for i := 0; i < n; i++ {
		for _, w := range g.adj[i] {
			m2 += w
		}
	}

	// Copy adjacency
	adj := make([]map[int]float64, n)
	for i := 0; i < n; i++ {
		adj[i] = make(map[int]float64, len(g.adj[i]))
		for k, v := range g.adj[i] {
			adj[i][k] = v
		}
	}

	// Node -> community mapping (tracks original nodes throughout)
	// originalComm[origNode] = current community
	originalComm := make([]int, n)
	for i := 0; i < n; i++ {
		originalComm[i] = i
	}

	// Weighted degree of each node
	kDeg := make([]float64, n)
	for i := 0; i < n; i++ {
		for _, w := range adj[i] {
			kDeg[i] += w
		}
	}

	curN := n
	curAdj := adj
	curKDeg := kDeg
	curM2 := m2

	prevModularity := -1.0

	for pass := 0; pass < maxPasses; pass++ {
		// Local phase: each node in its own community initially within this level
		comm := make([]int, curN)
		for i := 0; i < curN; i++ {
			comm[i] = i
		}

		sumTot := make([]float64, curN)
		copy(sumTot, curKDeg)

		improved := false
		for changed := true; changed; {
			changed = false
			for i := 0; i < curN; i++ {
				oldComm := comm[i]
				ki := curKDeg[i]

				// Compute sum of weights from i to each neighboring community
				neighComms := make(map[int]float64)
				for j, w := range curAdj[i] {
					neighComms[comm[j]] += w
				}

				// Remove i from its current community for evaluation
				sumTot[oldComm] -= ki

				bestComm := oldComm
				bestDelta := 0.0

				// Evaluate all candidate communities (including oldComm)
				for c, sumIn := range neighComms {
					delta := 2*sumIn/curM2 - 2*ki*sumTot[c]/(curM2*curM2)
					if delta > bestDelta || (delta == bestDelta && c == oldComm) {
						bestDelta = delta
						bestComm = c
					}
				}

				// Also evaluate staying in oldComm if it wasn't a neighbor
				if _, ok := neighComms[oldComm]; !ok {
					// No edges to own community members — delta is 0
					if 0 >= bestDelta {
						bestComm = oldComm
					}
				}

				comm[i] = bestComm
				sumTot[bestComm] += ki

				if bestComm != oldComm {
					changed = true
					improved = true
				}
			}
		}

		if !improved {
			break
		}

		// Check modularity improvement against minDelta
		curMod := computeModularityFromAdj(curAdj, comm, curN, curM2)
		if prevModularity >= 0 && curMod-prevModularity < minDelta {
			break
		}
		prevModularity = curMod

		// Remap community IDs to sequential
		commMap := make(map[int]int)
		nextID := 0
		for i := 0; i < curN; i++ {
			if _, ok := commMap[comm[i]]; !ok {
				commMap[comm[i]] = nextID
				nextID++
			}
			comm[i] = commMap[comm[i]]
		}

		if nextID >= curN {
			break
		}

		// Update original node -> community mapping
		newOrigComm := make([]int, n)
		for origNode := 0; origNode < n; origNode++ {
			// originalComm[origNode] is the super-node index in the current level
			superNode := originalComm[origNode]
			newOrigComm[origNode] = comm[superNode]
		}
		originalComm = newOrigComm

		// Contract graph: merge nodes in the same community into super-nodes.
		// To avoid double-counting undirected edges, only add each edge once
		// (from the lower-indexed endpoint).
		newN := nextID
		newAdj := make([]map[int]float64, newN)
		for i := 0; i < newN; i++ {
			newAdj[i] = make(map[int]float64)
		}
		newKDeg := make([]float64, newN)

		for u := 0; u < curN; u++ {
			su := comm[u]
			newKDeg[su] += curKDeg[u]
			for v, w := range curAdj[u] {
				if u > v {
					continue // only process each undirected edge once
				}
				sv := comm[v]
				if su != sv {
					newAdj[su][sv] += w
					newAdj[sv][su] += w
				}
			}
		}

		curN = newN
		curAdj = newAdj
		curKDeg = newKDeg
		// m2 stays constant across passes (total edge weight doesn't change)
	}

	// Build result
	commIDs := make(map[int]bool)
	result := &LouvainResult{
		Communities: make(map[int]int, n),
	}
	for i := 0; i < n; i++ {
		result.Communities[i] = originalComm[i]
		commIDs[originalComm[i]] = true
	}
	result.NumCommunities = len(commIDs)
	result.Modularity = computeModularityFromAdj(g.adj, originalComm, n, m2)

	return result
}

// computeModularityFromAdj calculates Q = (1/m2) * sum_ij [ A_ij - k_i*k_j/m2 ] * delta(c_i, c_j)
// where m2 = sum of all adjacency entries = 2 * total_edge_weight for undirected graphs.
func computeModularityFromAdj(adj []map[int]float64, comm []int, n int, m2 float64) float64 {
	if m2 == 0 {
		return 0
	}

	kDeg := make([]float64, n)
	for i := 0; i < n; i++ {
		for _, w := range adj[i] {
			kDeg[i] += w
		}
	}

	var q float64
	for i := 0; i < n; i++ {
		for j, aij := range adj[i] {
			if comm[i] == comm[j] {
				q += aij - kDeg[i]*kDeg[j]/m2
			}
		}
	}
	return q / m2
}
