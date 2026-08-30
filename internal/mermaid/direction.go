package mermaid

type direction genericCoord

var (
	Up         = direction{1, 0}
	Down       = direction{1, 2}
	Left       = direction{0, 1}
	Right      = direction{2, 1}
	UpperRight = direction{2, 0}
	UpperLeft  = direction{0, 0}
	LowerRight = direction{2, 2}
	LowerLeft  = direction{0, 2}
	Middle     = direction{1, 1}
)

func (self direction) getOpposite() direction {
	switch self {
	case Up:
		return Down
	case Down:
		return Up
	case Left:
		return Right
	case Right:
		return Left
	case UpperRight:
		return LowerLeft
	case UpperLeft:
		return LowerRight
	case LowerRight:
		return UpperLeft
	case LowerLeft:
		return UpperRight
	case Middle:
		return Middle
	}
	panic("Unknown direction")
}

func (self gridCoord) Direction(dir direction) gridCoord {
	return gridCoord{x: self.x + dir.x, y: self.y + dir.y}
}

func (self *graph) selfReferenceDirection() (direction, direction, direction, direction) {
	if self.graphDirection == "LR" {
		return Right, Down, Down, Right
	}
	return Down, Right, Right, Down
}

func (self *graph) determineStartAndEndDir(e *edge) (direction, direction, direction, direction) {
	if e.from == e.to {
		return self.selfReferenceDirection()
	}
	arrowDirection := determineDirection(genericCoord(*e.from.gridCoord), genericCoord(*e.to.gridCoord))
	var preferredDir, preferredOppositeDir, alternativeDir, alternativeOppositeDir direction

	isBackwards := (self.graphDirection == "LR" && (arrowDirection == Left || arrowDirection == UpperLeft || arrowDirection == LowerLeft)) ||
		(self.graphDirection != "LR" && (arrowDirection == Up || arrowDirection == UpperLeft || arrowDirection == UpperRight))

	switch arrowDirection {
	case LowerRight:
		if self.graphDirection == "LR" {
			preferredDir = Down
			preferredOppositeDir = Left
			alternativeDir = Right
			alternativeOppositeDir = Up
		} else {
			preferredDir = Right
			preferredOppositeDir = Up
			alternativeDir = Down
			alternativeOppositeDir = Left
		}
	case UpperRight:
		if self.graphDirection == "LR" {
			preferredDir = Up
			preferredOppositeDir = Left
			alternativeDir = Right
			alternativeOppositeDir = Down
		} else {
			preferredDir = Right
			preferredOppositeDir = Down
			alternativeDir = Up
			alternativeOppositeDir = Left
		}
	case LowerLeft:
		if self.graphDirection == "LR" {
			preferredDir = Down
			preferredOppositeDir = Down
			alternativeDir = Left
			alternativeOppositeDir = Up
		} else {
			preferredDir = Left
			preferredOppositeDir = Up
			alternativeDir = Down
			alternativeOppositeDir = Right
		}
	case UpperLeft:
		if self.graphDirection == "LR" {
			preferredDir = Down
			preferredOppositeDir = Down
			alternativeDir = Left
			alternativeOppositeDir = Down
		} else {
			preferredDir = Right
			preferredOppositeDir = Right
			alternativeDir = Up
			alternativeOppositeDir = Right
		}
	default:
		if isBackwards {
			switch {
			case self.graphDirection == "LR" && arrowDirection == Left:
				preferredDir = Down
				preferredOppositeDir = Down
				alternativeDir = Left
				alternativeOppositeDir = Right
			case self.graphDirection == "TD" && arrowDirection == Up:
				preferredDir = Right
				preferredOppositeDir = Right
				alternativeDir = Up
				alternativeOppositeDir = Down
			default:
				preferredDir = arrowDirection
				preferredOppositeDir = preferredDir.getOpposite()
				alternativeDir = arrowDirection
				alternativeOppositeDir = preferredOppositeDir
			}
		} else {
			preferredDir = arrowDirection
			preferredOppositeDir = preferredDir.getOpposite()
			alternativeDir = arrowDirection
			alternativeOppositeDir = preferredOppositeDir
		}
	}
	return preferredDir, preferredOppositeDir, alternativeDir, alternativeOppositeDir
}
