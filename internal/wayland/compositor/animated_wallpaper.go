package compositor

import "fyshos.com/tyde/internal/wallpaper"

// Re-export AnimatedWallpaper from the shared package for use in this package.
type (
	animatedWallpaper = wallpaper.AnimatedWallpaper
	matrixAnim        = wallpaper.MatrixAnim
	starfieldAnim     = wallpaper.StarfieldAnim
)
