package behavior_test

// Behavior tests for issue #128. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-128.md. The deliverable is a picture, so the scenarios
// measure it rather than describe it: two renderings of one picture agree
// cell by cell on a coarse grid, and two different pictures do not.

import (
	"bytes"
	"image"
	_ "image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// appIcon is the master Wails generates the platform icons from, relative to
// the repository root, and logoSource is the artwork it is made from.
const (
	appIcon    = "cmd/owl-desktop/build/appicon.png"
	logoSource = "docs/assets/coding-owl-logo.png"
)

// iconSize is what Wails takes as the master: it generates the macOS .icns and
// the Windows .ico from this one file, so it has to hold the largest size
// either of them needs.
const iconSize = 1024

// gridSide is how coarse a grid two pictures are compared on. Sixteen cells a
// side is fine enough that a different picture cannot agree by accident, and
// coarse enough that a resample or a re-encode does not disagree.
const gridSide = 16

// sameArtwork is how far two renderings of one picture may drift in any cell
// and any channel, of 255. A resample of the logo lands within about 2; a
// different picture is hundreds away.
const sameArtwork = 12

// readImage decodes a PNG under the repository. It fails the scenario outright
// when it is not there or is not an image: every measurement of it would be
// vacuously true.
func readImage(t *testing.T, rel string) image.Image {
	t.Helper()
	f, err := os.Open(filepath.Join(repoDir, rel))
	if err != nil {
		t.Fatalf("opening %s: %v", rel, err)
	}
	defer func() { _ = f.Close() }()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("%s is not an image Go can read: %v", rel, err)
	}
	return img
}

// cell is one cell of a coarse grid: the average colour of the part of the
// picture that falls in it.
type cell struct{ r, g, b float64 }

// coarse averages a picture down to side x side cells. It is the whole trick
// these scenarios turn on: it throws away the detail two renderings disagree
// about and keeps the shape they agree on.
func coarse(img image.Image, side int) []cell {
	out := make([]cell, side*side)
	n := make([]float64, side*side)
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			i := (y-b.Min.Y)*side/b.Dy()*side + (x-b.Min.X)*side/b.Dx()
			r, g, bl, _ := img.At(x, y).RGBA()
			out[i].r += float64(r >> 8)
			out[i].g += float64(g >> 8)
			out[i].b += float64(bl >> 8)
			n[i]++
		}
	}
	for i := range out {
		out[i].r /= n[i]
		out[i].g /= n[i]
		out[i].b /= n[i]
	}
	return out
}

// widestGap is the largest disagreement between two grids, in any cell and any
// channel, of 255.
func widestGap(a, b []cell) float64 {
	var worst float64
	for i := range a {
		for _, d := range []float64{
			math.Abs(a[i].r - b[i].r),
			math.Abs(a[i].g - b[i].g),
			math.Abs(a[i].b - b[i].b),
		} {
			if d > worst {
				worst = d
			}
		}
	}
	return worst
}

// luma is a cell's brightness, of 255.
func (c cell) luma() float64 { return 0.2126*c.r + 0.7152*c.g + 0.0722*c.b }

// spread is how much the brightness of a picture's cells varies at that
// coarseness - its standard deviation, of 255. A picture that has gone to mush
// at a size has a small one, because every cell has become the same average
// smudge.
func spread(img image.Image, side int) float64 {
	cells := coarse(img, side)
	var sum, sq float64
	for _, c := range cells {
		sum += c.luma()
		sq += c.luma() * c.luma()
	}
	mean := sum / float64(len(cells))
	return math.Sqrt(sq/float64(len(cells)) - mean*mean)
}

// visibleLuma is the mean brightness of a picture and the share of it that is
// near-white, counting only the pixels you can see. The Wails placeholder was
// a white card, so both say whether it is still there without needing the
// deleted file to compare against.
func visibleLuma(img image.Image) (mean, nearWhite float64) {
	b := img.Bounds()
	var sum, white, visible float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if a>>8 < 128 {
				continue
			}
			visible++
			l := 0.2126*float64(r>>8) + 0.7152*float64(g>>8) + 0.0722*float64(bl>>8)
			sum += l
			if l > 230 {
				white++
			}
		}
	}
	if visible == 0 {
		return 0, 0
	}
	return sum / visible, white / visible
}

func TestS1IconIsWhereWailsLooksForItAtTheMasterSize(t *testing.T) {
	img := readImage(t, appIcon)

	b := img.Bounds()

	if b.Dx() != iconSize || b.Dy() != iconSize {
		t.Errorf("%s is %dx%d, want %dx%d: Wails generates the platform icons from this master",
			appIcon, b.Dx(), b.Dy(), iconSize, iconSize)
	}
}

func TestS2IconIsNoLongerTheWailsPlaceholder(t *testing.T) {
	img := readImage(t, appIcon)

	mean, nearWhite := visibleLuma(img)

	// The placeholder was about two thirds near-white and half as bright
	// again as the artwork; both of these put real distance between them.
	if nearWhite >= 0.2 {
		t.Errorf("%.0f%% of %s is near-white; the Wails placeholder's white card was two thirds",
			100*nearWhite, appIcon)
	}
	if mean >= 90 {
		t.Errorf("%s has a mean luminance of %.0f of 255, which is the bright card the placeholder was",
			appIcon, mean)
	}
}

func TestS3IconIsTheCodingOwlLogo(t *testing.T) {
	icon := readImage(t, appIcon)
	logo := readImage(t, logoSource)

	gap := widestGap(coarse(icon, gridSide), coarse(logo, gridSide))

	if gap > sameArtwork {
		t.Errorf("%s and %s differ by %.0f of 255 somewhere on a %dx%d grid, want at most %d: "+
			"the app icon is not that artwork", appIcon, logoSource, gap, gridSide, gridSide, sameArtwork)
	}
}

func TestS4IconStillReadsAtTheSizesItIsDrawn(t *testing.T) {
	img := readImage(t, appIcon)

	for _, side := range []int{16, 32} {
		if got := spread(img, side); got <= 20 {
			t.Errorf("at %dx%d the icon's cells vary by only %.0f of 255; it has gone flat at a size "+
				"a dock or a menu bar draws it", side, side, got)
		}
	}
}

func TestS5PackagedAppCarriesTheIcon(t *testing.T) {
	if os.Getenv("OWL_DESKTOP_BUILD") == "" {
		t.Skip("set OWL_DESKTOP_BUILD=1 to build the desktop app; it downloads the frontend's dependencies")
	}
	cmd := exec.Command("make", "desktop")
	cmd.Dir = repoDir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("make desktop: %v", err)
	}

	bundles, _ := filepath.Glob(filepath.Join(repoDir, "cmd", "owl-desktop", "build", "bin", "*.app"))
	if len(bundles) == 0 {
		t.Fatal("make desktop left no app bundle under cmd/owl-desktop/build/bin")
	}
	icns := filepath.Join(bundles[0], "Contents", "Resources", "iconfile.icns")
	data, err := os.ReadFile(icns)
	if err != nil {
		t.Fatalf("the app bundle carries no icon: %v", err)
	}

	packaged := largestPNGIn(t, data)
	logo := readImage(t, logoSource)

	gap := widestGap(coarse(packaged, gridSide), coarse(logo, gridSide))
	if gap > sameArtwork {
		t.Errorf("the icon in %s differs from %s by %.0f of 255 on a %dx%d grid, want at most %d",
			icns, logoSource, gap, gridSide, gridSide, sameArtwork)
	}
}

// largestPNGIn decodes the biggest picture embedded in an .icns. The format is
// a header and then typed chunks, and every size Wails writes is a PNG, so the
// pictures are found by their own signature rather than by parsing the
// container.
func largestPNGIn(t *testing.T, data []byte) image.Image {
	t.Helper()
	signature := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	var best image.Image
	var bestArea int
	for at := 0; ; {
		i := bytes.Index(data[at:], signature)
		if i < 0 {
			break
		}
		at += i
		img, _, err := image.Decode(bytes.NewReader(data[at:]))
		if err == nil {
			if area := img.Bounds().Dx() * img.Bounds().Dy(); area > bestArea {
				best, bestArea = img, area
			}
		}
		at += len(signature)
	}
	if best == nil {
		t.Fatal("no picture could be read out of the .icns")
	}
	return best
}

func TestS6BuildNotesSayHowTheIconWasMade(t *testing.T) {
	notes := readFile(t, filepath.Join(repoDir, "cmd/owl-desktop/build/README.md"))

	if !strings.Contains(notes, logoSource) {
		t.Errorf("cmd/owl-desktop/build/README.md does not name %s as the source of the icon:\n%s",
			logoSource, notes)
	}
	// A command, so the icon can be remade rather than guessed at. sips is
	// macOS's own, which is the platform this app is built on (ADR-0002).
	if !strings.Contains(notes, "sips") {
		t.Errorf("cmd/owl-desktop/build/README.md gives no command that derives the icon from the logo:\n%s", notes)
	}
	if !strings.Contains(notes, "appicon.png") {
		t.Errorf("cmd/owl-desktop/build/README.md does not say the command writes appicon.png:\n%s", notes)
	}
}
