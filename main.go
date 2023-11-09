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
	"github.com/go-pg/pg/v10"
	"github.com/go-pg/pg/v10/orm"
	"github.com/google/uuid"
)

const FILES = "./files"

type File struct {
	ID          string `json:"id"`
	Size        int64  `json:"size"`
	Filename    string `json:"name"`
	ContentType string `json:"type"`
}

var db *pg.DB
var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func main() {
	r := gin.Default()
	db = pg.Connect(&pg.Options{
		User:     "postgres",
		Password: "epic",
	})
	defer db.Close()

	models := []interface{}{
		(*File)(nil),
	}

	for _, model := range models {
		err := db.Model(model).CreateTable(&orm.CreateTableOptions{
			IfNotExists: true,
		})
		if err != nil {
			panic(err)
		}
	}

	r.Use(static.Serve("/", static.LocalFile("static", false)))

	r.GET("/files/:id", func(c *gin.Context) {
		file := File{ID: c.Param("id")}

		err := db.Model(&file).Select()
		if err != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		c.Header("Content-Type", file.ContentType)
		c.Header("Content-Disposition", "filename=\""+quoteEscaper.Replace(file.Filename)+"\"")
		c.File(filepath.Join(FILES, file.ID))
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

		_, err = db.Model(f).Insert()
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}

		c.JSON(http.StatusOK, f)
	})

	r.Run(":8080")
}
