package main

import (
	"fmt"
	"io"

	"math/rand"
)

// Edge represents a directed edge from a node to another node.
type Edge struct {
	From int
	To   int
}

// GenerateRandomDAG generates a random DAG with nodeCount nodes and edgeCount edges.
// The graph will always have a single root node.
func GenerateRandomDAG(nodeCount, edgeCount int) ([]Edge, int, error) {
	if edgeCount > nodeCount*(nodeCount-1)/2 {
		return nil, -1, fmt.Errorf("too many edges for a DAG with %d nodes", nodeCount)
	}

	// Generates a random order of nodes
	nodes := make([]int, nodeCount)
	for i := 0; i < nodeCount; i++ {
		nodes[i] = i
	}
	rand.Shuffle(nodeCount, func(i, j int) {
		nodes[i], nodes[j] = nodes[j], nodes[i]
	})

	rootNode := nodes[0]

	// Initialize edges and adjacency list
	var edges []Edge
	adjacencyList := make(map[int][]int)
	inDegree := make([]int, nodeCount)

	// Ensure the graph is connected by making sure every node has an incoming edge (except root)
	for i := 1; i < nodeCount; i++ {
		fromIndex := rand.Intn(i) // Randomly connect to one of the earlier nodes
		from := nodes[fromIndex]
		to := nodes[i]
		e := Edge{From: from, To: to}
		edges = append(edges, e)
		adjacencyList[from] = append(adjacencyList[from], to)
		inDegree[to]++
	}

	// Now, generate all possible edges from earlier to later nodes, excluding already existing edges
	totalPossibleEdges := []Edge{}
	existingEdges := make(map[Edge]bool)
	// Mark existing edges
	for _, e := range edges {
		existingEdges[e] = true
	}

	for i := 0; i < nodeCount; i++ {
		for j := i + 1; j < nodeCount; j++ {
			e := Edge{From: nodes[i], To: nodes[j]}
			if !existingEdges[e] {
				totalPossibleEdges = append(totalPossibleEdges, e)
			}
		}
	}

	// Shuffle totalPossibleEdges
	rand.Shuffle(len(totalPossibleEdges), func(i, j int) {
		totalPossibleEdges[i], totalPossibleEdges[j] = totalPossibleEdges[j], totalPossibleEdges[i]
	})

	// Add edges until we have edgeCount edges
	edgeNeeded := edgeCount - len(edges)
	for i := 0; i < len(totalPossibleEdges) && edgeNeeded > 0; i++ {
		e := totalPossibleEdges[i]
		edges = append(edges, e)
		adjacencyList[e.From] = append(adjacencyList[e.From], e.To)
		inDegree[e.To]++
		edgeNeeded--
	}

	return edges, rootNode, nil
}

// printDot outputs the edges in DOT format for visualization.
func printDot(out io.Writer, edges []Edge) {
	fmt.Fprintln(out, "digraph {")
	for _, edge := range edges {
		fmt.Fprintf(out, "  %d -> %d;\n", edge.From, edge.To)
	}
	fmt.Fprintln(out, "}")
}
