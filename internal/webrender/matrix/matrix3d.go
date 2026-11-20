package matrix

// Plik dodany w gopdfgo, nie pochodzi ze źródła webrender.

import "math"

// Matrix4 to macierz przekształcenia 3D z CSS Transforms 2 § 12, zapisana wierszami: punkt jest
// wektorem kolumnowym (x, y, z, 1), a M[w][k] to element w wierszu w i kolumnie k. Liczymy w
// float64, bo przy składaniu kilku obrotów błąd float32 widać na krawędziach.
type Matrix4 [4][4]float64

// Identity4 zwraca macierz jednostkową.
func Identity4() Matrix4 {
	return Matrix4{{1, 0, 0, 0}, {0, 1, 0, 0}, {0, 0, 1, 0}, {0, 0, 0, 1}}
}

// FromTransform zanurza przekształcenie 2D w 3D (CSS Transforms 2 § 12, matrix()).
func FromTransform(t Transform) Matrix4 {
	return Matrix4{
		{float64(t.A), float64(t.C), 0, float64(t.E)},
		{float64(t.B), float64(t.D), 0, float64(t.F)},
		{0, 0, 1, 0},
		{0, 0, 0, 1},
	}
}

// FromColumns buduje macierz z 16 liczb matrix3d(), podanych kolumnami (CSS Transforms 2 § 12).
func FromColumns(v [16]float64) Matrix4 {
	var m Matrix4
	for col := 0; col < 4; col++ {
		for row := 0; row < 4; row++ {
			m[row][col] = v[col*4+row]
		}
	}
	return m
}

// Translation3D zwraca przesunięcie translate3d(tx, ty, tz).
func Translation3D(tx, ty, tz float64) Matrix4 {
	m := Identity4()
	m[0][3], m[1][3], m[2][3] = tx, ty, tz
	return m
}

// Scaling3D zwraca skalowanie scale3d(sx, sy, sz).
func Scaling3D(sx, sy, sz float64) Matrix4 {
	m := Identity4()
	m[0][0], m[1][1], m[2][2] = sx, sy, sz
	return m
}

// Rotation3D zwraca obrót rotate3d(x, y, z, angle) o kąt w radianach wokół osi (x, y, z), według
// wzoru z CSS Transforms 2 § 12. Oś zerowa daje macierz jednostkową (obrót nie jest stosowany).
func Rotation3D(x, y, z, angle float64) Matrix4 {
	length := math.Sqrt(x*x + y*y + z*z)
	if length == 0 {
		return Identity4()
	}
	x, y, z = x/length, y/length, z/length
	sc := math.Sin(angle/2) * math.Cos(angle/2)
	sq := math.Sin(angle/2) * math.Sin(angle/2)
	return Matrix4{
		{1 - 2*(y*y+z*z)*sq, 2 * (x*y*sq - z*sc), 2 * (x*z*sq + y*sc), 0},
		{2 * (x*y*sq + z*sc), 1 - 2*(x*x+z*z)*sq, 2 * (y*z*sq - x*sc), 0},
		{2 * (x*z*sq - y*sc), 2 * (y*z*sq + x*sc), 1 - 2*(x*x+y*y)*sq, 0},
		{0, 0, 0, 1},
	}
}

// Mul4 zwraca iloczyn m * n: najpierw n, potem m.
func Mul4(m, n Matrix4) Matrix4 {
	var out Matrix4
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			var s float64
			for k := 0; k < 4; k++ {
				s += m[i][k] * n[k][j]
			}
			out[i][j] = s
		}
	}
	return out
}

// Flatten zwraca rzut prostokątny przekształcenia na płaszczyznę rodzica, czyli część 2D macierzy
// (m11, m12, m21, m22, m41, m42 w zapisie kolumnowym CSS). Tak rysuje element transform-style: flat
// bez perspektywy (CSS Transforms 2 § 6, „flattening”); składowe z nie wpływają na obraz. Ostatni
// wiersz macierzy ma postać (0, 0, 0, w) (perspektywę pomija już składanie), więc współrzędne dzielimy
// przez w, jak przy przejściu ze współrzędnych jednorodnych.
func (m Matrix4) Flatten() Transform {
	w := m[3][3]
	if w == 0 {
		return Transform{}
	}
	return Transform{
		A: fl(m[0][0] / w), B: fl(m[1][0] / w),
		C: fl(m[0][1] / w), D: fl(m[1][1] / w),
		E: fl(m[0][3] / w), F: fl(m[1][3] / w),
	}
}

// linearDeterminant to wyznacznik części liniowej 3×3 (bez perspektywy, którą pomijamy).
func (m Matrix4) linearDeterminant() float64 {
	return m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1]) -
		m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0]) +
		m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])
}

// Invertible mówi, czy macierz jest odwracalna; element o macierzy nieodwracalnej nie jest
// rysowany (CSS Transforms 1 § 6, CSS Transforms 2 § 6).
func (m Matrix4) Invertible() bool { return m.linearDeterminant() != 0 && m[3][3] != 0 }

// BackfaceVisible mówi, czy widać tylną stronę elementu (CSS Transforms 2 § 11): tak jest, gdy
// element w macierzy odwrotnej w wierszu 3 i kolumnie 3 jest ujemny, tzn. wektor normalny płaszczyzny
// elementu po przekształceniu jest odwrócony od widza. Bez perspektywy ten element to dopełnienie
// algebraiczne (m11·m22 − m12·m21) podzielone przez wyznacznik części liniowej.
func (m Matrix4) BackfaceVisible() bool {
	det := m.linearDeterminant()
	if det == 0 {
		return false
	}
	return (m[0][0]*m[1][1]-m[0][1]*m[1][0])/det < 0
}
