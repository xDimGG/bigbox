package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const FILES = "./files"

type File struct {
	ID          string `json:"id"`
	Size        int64  `json:"size"`
	Filename    string `json:"name"`
	ContentType string `json:"type"`
}

var db []*File
var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func main() {
	r := gin.Default()

	r.Use(static.Serve("/", static.LocalFile("static", false)))

	r.GET("/files/:id", func(c *gin.Context) {
		for _, file := range db {
			if file.ID == c.Param("id") {
				// c.JSON(http.StatusOK, file)

				c.Header("Content-Type", file.ContentType)
				c.Header("Content-Disposition", "filename=\""+quoteEscaper.Replace(file.Filename)+"\"")
				c.File(filepath.Join(FILES, file.ID))
				return
			}
		}

		c.AbortWithStatus(http.StatusNotFound)
	})

	r.POST("/files", func(c *gin.Context) {
		header, err := c.FormFile("file")
		if err != nil {
			c.AbortWithError(http.StatusBadRequest, err)
			return
		}

		spew.Dump(header.Header)

		userFile, err := header.Open()
		if err != nil {
			c.AbortWithError(http.StatusBadRequest, err)
			return
		}

		fmt.Println("copying " + header.Filename)

		id := uuid.NewString()
		localFile, err := os.Create(filepath.Join(FILES, id))
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}

		_, err = io.Copy(localFile, userFile)
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}

		s, err := url.QueryUnescape(header.Filename)
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}

		f := &File{
			ID:          id,
			Size:        header.Size,
			Filename:    s,
			ContentType: header.Header.Get("Content-Type"),
		}
		db = append(db, f)
		c.JSON(http.StatusOK, f)
	})

	r.Run(":8080")
}
