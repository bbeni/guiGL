// Copyright 2014 The go-gl Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
// Renders a textured spinning cube using GLFW 3 and OpenGL 4.1 core forward-compatible profile.
//
// Modifications:
// Copyright 2024 Benjamin Froelich bbenif@gmail.com
// MIT licence

package main

import (
	"fmt"
	"time"
	"image"
	"image/draw"
	"image/color"

	"github.com/bbeni/guiGL/win"
	"github.com/faiface/mainthread"
)

const (
	rectWidth    = 200
	rectHeight   = 103
	windowWidth  = 1280
	windowHeight = 721
)

func colors(index uint8) color.RGBA {
	if 0 <= index && index < 5 { return color.RGBA{index*50, 255 - index*50, 30, 255} }
	if index == 5 {	return color.RGBA{255, 255, 255, 255} }
	return color.RGBA{0, 0, 0, 255}
}

func run() {
	w, err := win.New(
		win.Title("openGL/gui"),
		win.Size(windowWidth, windowHeight))

	if err != nil {
		panic(err)
	}

	drawRect := func(index uint8) func(draw.Image) image.Rectangle {
		return func(drw draw.Image) image.Rectangle {
			r := image.Rect(windowWidth-rectWidth, int(index)*rectHeight, windowWidth, int(index+1)*rectHeight)
			draw.Draw(drw, r, image.NewUniform(colors(index)), image.ZP, draw.Src)
			return r
		}
	}

	w.Draw() <- drawRect(0)
	w.Draw() <- drawRect(1)
	w.Draw() <- drawRect(2)
	w.Draw() <- drawRect(3)
	w.Draw() <- drawRect(4)
	w.Draw() <- drawRect(5)

	w.GL() <- CubeInit // send it to GL chanel so we have gl context in later calls
	w.GL() <- CubeDraw

	loop:
	for {
		select {
		case event, _ := <-w.Events():
			switch event := event.(type) {
			case win.WiClose, win.KbDown:
				break loop
			case win.MoDown:
				if event.Point.X > windowWidth - rectWidth {
					colorIndex := uint8(event.Point.Y/rectHeight)
					CubeClearColor = colors(colorIndex)
				}
			}
		default:
			w.GL() <- CubeDraw
		}
	}

	var _ = time.Sleep
	var _ = fmt.Print

	close(w.Draw())
}

func main() {
	mainthread.Run(run)
}