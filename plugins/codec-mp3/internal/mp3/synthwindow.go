package mp3

func synthWindow(workspace []float32, zLineOffset, index int, window []float32) (a, b [4]float32) {
	load := func(k int) (float32, float32, int, int) {
		vZeroIndex := zLineOffset + 4*index - k*64
		vYIndex := zLineOffset + 4*index - (15-k)*64
		return window[2*k], window[2*k+1], vZeroIndex, vYIndex
	}
	{
		w0, w1, vZeroIndex, vYIndex := load(0)
		for j := 0; j < 4; j++ {
			b[j] = workspace[vZeroIndex+j]*w1 + workspace[vYIndex+j]*w0
			a[j] = workspace[vZeroIndex+j]*w0 - workspace[vYIndex+j]*w1
		}
	}
	{
		w0, w1, vZeroIndex, vYIndex := load(1)
		for j := 0; j < 4; j++ {
			b[j] += workspace[vZeroIndex+j]*w1 + workspace[vYIndex+j]*w0
			a[j] += workspace[vYIndex+j]*w1 - workspace[vZeroIndex+j]*w0
		}
	}
	{
		w0, w1, vZeroIndex, vYIndex := load(2)
		for j := 0; j < 4; j++ {
			b[j] += workspace[vZeroIndex+j]*w1 + workspace[vYIndex+j]*w0
			a[j] += workspace[vZeroIndex+j]*w0 - workspace[vYIndex+j]*w1
		}
	}
	{
		w0, w1, vZeroIndex, vYIndex := load(3)
		for j := 0; j < 4; j++ {
			b[j] += workspace[vZeroIndex+j]*w1 + workspace[vYIndex+j]*w0
			a[j] += workspace[vYIndex+j]*w1 - workspace[vZeroIndex+j]*w0
		}
	}
	{
		w0, w1, vZeroIndex, vYIndex := load(4)
		for j := 0; j < 4; j++ {
			b[j] += workspace[vZeroIndex+j]*w1 + workspace[vYIndex+j]*w0
			a[j] += workspace[vZeroIndex+j]*w0 - workspace[vYIndex+j]*w1
		}
	}
	{
		w0, w1, vZeroIndex, vYIndex := load(5)
		for j := 0; j < 4; j++ {
			b[j] += workspace[vZeroIndex+j]*w1 + workspace[vYIndex+j]*w0
			a[j] += workspace[vYIndex+j]*w1 - workspace[vZeroIndex+j]*w0
		}
	}
	{
		w0, w1, vZeroIndex, vYIndex := load(6)
		for j := 0; j < 4; j++ {
			b[j] += workspace[vZeroIndex+j]*w1 + workspace[vYIndex+j]*w0
			a[j] += workspace[vZeroIndex+j]*w0 - workspace[vYIndex+j]*w1
		}
	}
	{
		w0, w1, vZeroIndex, vYIndex := load(7)
		for j := 0; j < 4; j++ {
			b[j] += workspace[vZeroIndex+j]*w1 + workspace[vYIndex+j]*w0
			a[j] += workspace[vYIndex+j]*w1 - workspace[vZeroIndex+j]*w0
		}
	}
	return a, b
}
