package win

import (
	"image"
	"image/draw"
	"image/color"

	"runtime"
	"time"
	"strings"
	"fmt"

	"github.com/bbeni/guiGL"

	"github.com/faiface/mainthread"
	"github.com/go-gl/gl/v4.2-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
)

// Option is a functional option to the window constructor New.
type Option func(*options)

type options struct {
	title         string
	width, height int
	resizable     bool
	borderless    bool
	maximized     bool
}

// Title option sets the title (caption) of the window.
func Title(title string) Option {
	return func(o *options) {
		o.title = title
	}
}

// Size option sets the width and height of the window.
func Size(width, height int) Option {
	return func(o *options) {
		o.width = width
		o.height = height
	}
}

// Resizable option makes the window resizable by the user.
func Resizable() Option {
	return func(o *options) {
		o.resizable = true
	}
}

// Borderless option makes the window borderless.
func Borderless() Option {
	return func(o *options) {
		o.borderless = true
	}
}

// Maximized option makes the window start maximized.
func Maximized() Option {
	return func(o *options) {
		o.maximized = true
	}
}

// New creates a new window with all the supplied options.
//
// The default title is empty and the default size is 640x480.
func New(opts ...Option) (*Win, error) {
	o := options{
		title:      "",
		width:      640,
		height:     480,
		resizable:  false,
		borderless: false,
		maximized:  false,
	}
	for _, opt := range opts {
		opt(&o)
	}

	eventsOut, eventsIn := gui.MakeEventsChan()

	w := &Win{
		eventsOut: eventsOut,
		eventsIn:  eventsIn,
		draw:      make(chan func(draw.Image) image.Rectangle),
		drawGL:    make(chan func()),
		newSize:   make(chan image.Rectangle),
		finish:    make(chan struct{}),
	}

	var err error
	mainthread.Call(func() {
		w.w, err = makeGLFWWin(&o)
	})
	if err != nil {
		return nil, err
	}

	mainthread.Call(func() {
		// hiDPI hack
		width, _ := w.w.GetFramebufferSize()
		w.ratio = width / o.width
		if w.ratio < 1 {
			w.ratio = 1
		}
		if w.ratio != 1 {
			o.width /= w.ratio
			o.height /= w.ratio
		}
		w.w.Destroy()
		w.w, err = makeGLFWWin(&o)
	})
	if err != nil {
		return nil, err
	}

	bounds := image.Rect(0, 0, o.width*w.ratio, o.height*w.ratio)
	w.img = image.NewRGBA(bounds)

	go func() {
		runtime.LockOSThread()
		w.openGLThread()
	}()

	mainthread.CallNonBlock(w.eventThread)

	return w, nil
}

func makeGLFWWin(o *options) (*glfw.Window, error) {
	err := glfw.Init()
	if err != nil {
		return nil, err
	}
	//glfw.WindowHint(glfw.DoubleBuffer, glfw.False)
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 2)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	if o.resizable {
		glfw.WindowHint(glfw.Resizable, glfw.True)
	} else {
		glfw.WindowHint(glfw.Resizable, glfw.False)
	}
	if o.borderless {
		glfw.WindowHint(glfw.Decorated, glfw.False)
	}
	if o.maximized {
		glfw.WindowHint(glfw.Maximized, glfw.True)
	}
	w, err := glfw.CreateWindow(o.width, o.height, o.title, nil, nil)
	if err != nil {
		return nil, err
	}
	if o.maximized {
		o.width, o.height = w.GetFramebufferSize() // set o.width and o.height to the window size due to the window being maximized
	}
	return w, nil
}

// Win is an Env that handles an actual graphical window.
//
// It receives its events from the OS and it draws to the surface of the window.
//
// Warning: only one window can be open at a time
type Win struct {
	eventsOut <-chan gui.Event
	eventsIn  chan<- gui.Event
	draw      chan func(draw.Image) image.Rectangle
	drawGL    chan func()

	newSize chan image.Rectangle
	finish  chan struct{}

	w     *glfw.Window
	img   *image.RGBA
	ratio int

	// open gl stuff
	guiTexture uint32
	guiShader  uint32
	quadVao    uint32
}

// Events returns the events channel of the window.
func (w *Win) Events() <-chan gui.Event { return w.eventsOut }

// Draw returns the draw channel of the window.
func (w *Win) Draw() chan<- func(draw.Image) image.Rectangle { return w.draw }

// GL returns the Open GL draw channel of the window.
func (w *Win) GL() chan<- func() { return w.drawGL }

var buttons = map[glfw.MouseButton]Button{
	glfw.MouseButtonLeft:   ButtonLeft,
	glfw.MouseButtonRight:  ButtonRight,
	glfw.MouseButtonMiddle: ButtonMiddle,
}

var keys = map[glfw.Key]Key{
	glfw.KeyLeft:         KeyLeft,
	glfw.KeyRight:        KeyRight,
	glfw.KeyUp:           KeyUp,
	glfw.KeyDown:         KeyDown,
	glfw.KeyEscape:       KeyEscape,
	glfw.KeySpace:        KeySpace,
	glfw.KeyBackspace:    KeyBackspace,
	glfw.KeyDelete:       KeyDelete,
	glfw.KeyEnter:        KeyEnter,
	glfw.KeyTab:          KeyTab,
	glfw.KeyHome:         KeyHome,
	glfw.KeyEnd:          KeyEnd,
	glfw.KeyPageUp:       KeyPageUp,
	glfw.KeyPageDown:     KeyPageDown,
	glfw.KeyLeftShift:    KeyShift,
	glfw.KeyRightShift:   KeyShift,
	glfw.KeyLeftControl:  KeyCtrl,
	glfw.KeyRightControl: KeyCtrl,
	glfw.KeyLeftAlt:      KeyAlt,
	glfw.KeyRightAlt:     KeyAlt,
}

func (w *Win) eventThread() {
	var moX, moY int

	w.w.SetCursorPosCallback(func(_ *glfw.Window, x, y float64) {
		moX, moY = int(x), int(y)
		w.eventsIn <- MoMove{image.Pt(moX*w.ratio, moY*w.ratio)}
	})

	w.w.SetMouseButtonCallback(func(_ *glfw.Window, button glfw.MouseButton, action glfw.Action, mod glfw.ModifierKey) {
		b, ok := buttons[button]
		if !ok {
			return
		}
		switch action {
		case glfw.Press:
			w.eventsIn <- MoDown{image.Pt(moX*w.ratio, moY*w.ratio), b}
		case glfw.Release:
			w.eventsIn <- MoUp{image.Pt(moX*w.ratio, moY*w.ratio), b}
		}
	})

	w.w.SetScrollCallback(func(_ *glfw.Window, xoff, yoff float64) {
		w.eventsIn <- MoScroll{image.Pt(int(xoff), int(yoff))}
	})

	w.w.SetCharCallback(func(_ *glfw.Window, r rune) {
		w.eventsIn <- KbType{r}
	})

	w.w.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, _ glfw.ModifierKey) {
		k, ok := keys[key]
		if !ok {
			return
		}
		switch action {
		case glfw.Press:
			w.eventsIn <- KbDown{k}
		case glfw.Release:
			w.eventsIn <- KbUp{k}
		case glfw.Repeat:
			w.eventsIn <- KbRepeat{k}
		}
	})

	w.w.SetFramebufferSizeCallback(func(_ *glfw.Window, width, height int) {
		r := image.Rect(0, 0, width, height)
		w.newSize <- r
		w.eventsIn <- gui.Resize{Rectangle: r}
	})

	w.w.SetCloseCallback(func(_ *glfw.Window) {
		w.eventsIn <- WiClose{}
	})

	r := w.img.Bounds()
	w.eventsIn <- gui.Resize{Rectangle: r}

	for {
		select {
		case <-w.finish:
			close(w.eventsIn)
			w.w.Destroy()
			return
		default:
			glfw.WaitEventsTimeout(1.0 / 30)
		}
	}
}

func (w *Win) openGLThread() {
	w.w.MakeContextCurrent()

	w.openGLSetup()

	w.openGLRenderGui(w.img.Bounds())
	w.w.SwapBuffers()

loop:
	for {
		var totalR image.Rectangle

		select {
		case r := <-w.newSize:
			img := image.NewRGBA(r)
			draw.Draw(img, w.img.Bounds(), w.img, w.img.Bounds().Min, draw.Src)
			w.img = img
			totalR = totalR.Union(r)
			// update gui texture size
			gl.DeleteTextures(1, &w.guiTexture)
			w.guiTexture = newScreenTexture(img.Bounds().Dx(), img.Bounds().Dy())
		case d, ok := <-w.draw:
			if !ok {
				close(w.finish)
				return
			}
			r := d(w.img)
			totalR = totalR.Union(r)
		// just immediately run GL rendering
		// we know all internal gl stuff is initialized
		// TODO: ceck what we need to reset in internal flush to be able to render correctly
		case glFunc, ok := <-w.drawGL:
			if !ok {
				close(w.finish)
				return
			}
			glFunc()
			// for now rerender the gui each GL() call
			w.openGLRenderGui(totalR)
			w.w.SwapBuffers()
		}
		for {
			select {
			case <-time.After(time.Second / 960):
				w.openGLRenderGui(totalR)
				w.w.SwapBuffers()
				totalR = image.ZR
				continue loop
			case r := <-w.newSize:
				img := image.NewRGBA(r)
				draw.Draw(img, w.img.Bounds(), w.img, w.img.Bounds().Min, draw.Src)
				w.img = img
				totalR = totalR.Union(r)
				// update gui texture size
				gl.DeleteTextures(1, &w.guiTexture)
				w.guiTexture = newScreenTexture(img.Bounds().Dx(), img.Bounds().Dy())
			case d, ok := <-w.draw:
				if !ok {
					close(w.finish)
					return
				}
				r := d(w.img)
				totalR = totalR.Union(r)
			// just immediately run GL rendering
			// we know all internal gl stuff is initialized
			// TODO: ceck what we need to reset in internal flush to be able to render correctly
			case glFunc, ok := <-w.drawGL:
				if !ok {
					close(w.finish)
					return
				}
				glFunc()
				// for now rerender the gui each GL() call
				w.openGLRenderGui(totalR)
				w.w.SwapBuffers()
			}
		}
	}
}


// For now the we render using a transparent texture where the whole gui gets rendered and we scissor and
// only clear the depth buffer to keep other stuff on both buffers.
//
// Further Idea:
//   render everything Gui related closer to the camera, so we don't have to clear the buffer.
//   when another call is made to update gui we still have to clear the depth buffer..
// Other Idea:
//   So we need to make sure that the gui screen texture is transparent and the shader respects alpha.
//   Everytime we want to render a new rectangle (i.e. `env.Draw() <- ...` call), we just clear that
//   with open gl scissor. We should save the area and when renderGui is executed we clear just the depth bit.
//

func (w *Win) openGLRenderGui(r image.Rectangle) {

	//gl.Clear(gl.DEPTH_BUFFER_BIT | gl.COLOR_BUFFER_BIT)

	bounds := w.img.Bounds()
	r = r.Intersect(bounds)
	if r.Empty() {
		return
	}

	tmp := image.NewRGBA(r)
	draw.Draw(tmp, r, w.img, r.Min, draw.Src)

	gl.TextureSubImage2D(
		w.guiTexture,
		0,
		int32(r.Min.X),
		int32(r.Min.Y),
		int32(r.Dx()),
		int32(r.Dy()),
		gl.RGBA,
		gl.UNSIGNED_BYTE,
		gl.Ptr(tmp.Pix))

	// TODO: scissor array of rects?
	gl.Enable(gl.DEPTH_TEST)
	gl.DepthFunc(gl.LESS)

	//TODO: this is a dirty trick to draw the gui on both buffers
	//      double render and we are on the same buffer as before.
	for range 2 {
		_, hei := w.w.GetFramebufferSize()
		// TODO: might be wrong, need to add ceil/floor to the values.
		gl.Scissor(int32(r.Min.X), int32(hei) - int32(r.Max.Y), int32(r.Dx()), int32(r.Dy()))
		gl.Enable(gl.SCISSOR_TEST)
		gl.Clear(gl.DEPTH_BUFFER_BIT)
		gl.Disable(gl.SCISSOR_TEST)

		gl.UseProgram(w.guiShader)
		gl.Enable(gl.BLEND)
		gl.BlendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA)  		 // Assume premultiplied alpha
		//gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA) // Non-premultipled version

		gl.BindVertexArray(w.quadVao)
		gl.ActiveTexture(gl.TEXTURE0)
		gl.BindTexture(gl.TEXTURE_2D, w.guiTexture)
		gl.DrawArrays(gl.TRIANGLES, 0, 6*2*3)

		w.w.SwapBuffers()
	}

	gl.Disable(gl.BLEND)

	//gl.Disable(gl.DEPTH_TEST)
}

func (w *Win) openGLSetup() {
	var err error
	if err = gl.Init(); err != nil {
		panic(err)
	}

	var screenVertShader = `
		#version 420

		in vec3 vert;
		in vec2 vertTexCoord;
		out vec2 fragTexCoord;

		void main() {
			fragTexCoord = vertTexCoord;
			gl_Position = vec4(vert.xy, 0.0, 1.0);
		}
	` + "\x00"

	var screenFragShader = `
		#version 420

		uniform sampler2D tex;
		in vec2 fragTexCoord;

		out vec4 outputColor;

		void main() {
			outputColor = texture(tex, fragTexCoord);
		}
	` + "\x00"

	var quadVertices = []float32 {
		//  X, Y, Z, U, V
		-1.0,  1.0, 1.0,  0.0, 0.0,
		1.0,  -1.0, 1.0,  1.0, 1.0,
		-1.0, -1.0, 1.0,  0.0, 1.0,
		-1.0,  1.0, 1.0,  0.0, 0.0,
		1.0,   1.0, 1.0,  1.0, 0.0,
		1.0,  -1.0, 1.0,  1.0, 1.0,
	}

	w.guiShader, err = NewGLProgram(screenVertShader, screenFragShader)
	//gl.UseProgram(w.guiShader)

	wid, hei := w.w.GetFramebufferSize()
	w.guiTexture = newScreenTexture(wid, hei)
	textureUniform := gl.GetUniformLocation(w.guiShader, gl.Str("tex\x00"))
	gl.Uniform1i(textureUniform, 0)
	gl.BindFragDataLocation(w.guiShader, 0, gl.Str("outputColor\x00"))

	gl.GenVertexArrays(1, &w.quadVao)
	gl.BindVertexArray(w.quadVao)

	var vbo uint32
	gl.GenBuffers(1, &vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(quadVertices)*4, gl.Ptr(quadVertices), gl.STATIC_DRAW)

	vertAttrib := uint32(gl.GetAttribLocation(w.guiShader, gl.Str("vert\x00")))
	gl.EnableVertexAttribArray(vertAttrib)
	gl.VertexAttribPointerWithOffset(vertAttrib, 3, gl.FLOAT, false, 5*4, 0)

	texCoordAttrib := uint32(gl.GetAttribLocation(w.guiShader, gl.Str("vertTexCoord\x00")))
	gl.EnableVertexAttribArray(texCoordAttrib)
	gl.VertexAttribPointerWithOffset(texCoordAttrib, 2, gl.FLOAT, false, 5*4, 3*4)

	gl.ClearColor(1.0, 1.0, 0.0, 1.0)
}


func NewGLProgram(vertexShaderSource, fragmentShaderSource string) (uint32, error) {

	vertexShader, err := compileShader(vertexShaderSource, gl.VERTEX_SHADER)
	if err != nil {
		return 0, err
	}

	fragmentShader, err := compileShader(fragmentShaderSource, gl.FRAGMENT_SHADER)
	if err != nil {
		return 0, err
	}

	program := gl.CreateProgram()

	gl.AttachShader(program, vertexShader)
	gl.AttachShader(program, fragmentShader)
	gl.LinkProgram(program)

	var status int32
	gl.GetProgramiv(program, gl.LINK_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetProgramiv(program, gl.INFO_LOG_LENGTH, &logLength)

		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetProgramInfoLog(program, logLength, nil, gl.Str(log))

		return 0, fmt.Errorf("failed to link program: %v", log)
	}

	gl.DeleteShader(vertexShader)
	gl.DeleteShader(fragmentShader)

	return program, nil
}

func compileShader(source string, shaderType uint32) (uint32, error) {
	shader := gl.CreateShader(shaderType)
	csources, free := gl.Strs(source)

	gl.ShaderSource(shader, 1, csources, nil)
	free()
	gl.CompileShader(shader)

	var status int32
	gl.GetShaderiv(shader, gl.COMPILE_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetShaderiv(shader, gl.INFO_LOG_LENGTH, &logLength)

		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetShaderInfoLog(shader, logLength, nil, gl.Str(log))

		return 0, fmt.Errorf("failed to compile %v: %v", source, log)
	}

	return shader, nil
}

func newScreenTexture(width, height int) (uint32) {

	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	if rgba.Stride != rgba.Rect.Size().X*4 {
		panic("unsupported stride")
	}
	draw.Draw(rgba, rgba.Bounds(), image.NewUniform(color.RGBA{0,0,0,0}), image.Point{0, 0}, draw.Src)

	var texture uint32
	gl.GenTextures(1, &texture)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, texture)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.TexImage2D(
		gl.TEXTURE_2D,
		0,
		gl.RGBA,
		int32(rgba.Rect.Size().X),
		int32(rgba.Rect.Size().Y),
		0,
		gl.RGBA,
		gl.UNSIGNED_BYTE,
		gl.Ptr(rgba.Pix))

	return texture
}