package reference

import "math"

// Vec3 is a galactic coordinate in light years (journal StarPos order x, y, z).
type Vec3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Distance returns the 3D Euclidean distance between pointA and pointB.
func Distance(pointA, pointB Vec3) float64 {
	deltaX, deltaY, deltaZ := pointB.X-pointA.X, pointB.Y-pointA.Y, pointB.Z-pointA.Z
	return math.Sqrt(deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ)
}

