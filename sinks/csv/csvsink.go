package csv

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"

	autoctx "github.com/vladimirvivien/automi/api/context"
)

type CsvSink struct {
	filepath  string
	delimChar rune
	headers   []string

	file      *os.File
	input     <-chan interface{}
	snkWriter io.Writer
	csvWriter *csv.Writer
	log       *log.Logger
}

func New() *CsvSink {
	csv := &CsvSink{
		delimChar: ',',
	}
	return csv
}

func (c *CsvSink) WithWriter(writer io.Writer) *CsvSink {
	c.snkWriter = writer
	return c
}

func (c *CsvSink) WithFile(path string) *CsvSink {
	c.filepath = path
	return c
}

func (c *CsvSink) SetInput(in <-chan interface{}) {
	c.input = in
}

func (c *CsvSink) init(ctx context.Context) error {
	//extract log entry
	c.log = autoctx.GetLogger(ctx)

	if c.input == nil {
		return fmt.Errorf("Input attribute not set")
	}

	// establish defaults
	if c.delimChar == 0 {
		c.delimChar = ','
	}

	var writer io.Writer
	if c.snkWriter != nil {
		writer = c.snkWriter
		c.log.Print("using IO Writer sink")
	} else {
		file, err := os.Create(c.filepath)
		if err != nil {
			return fmt.Errorf("Failed to create file %s: %v ", c.filepath, err)
		}
		writer = file
		c.file = file
		c.log.Print("using sink file", file.Name())
	}
	c.csvWriter = csv.NewWriter(writer)
	c.csvWriter.Comma = c.delimChar

	// write headers
	if len(c.headers) > 0 {
		if err := c.csvWriter.Write(c.headers); err != nil {
			return err
		}
		c.log.Print("wrote headers [", c.headers, "]")
	}

	c.log.Print("component initialized")

	return nil
}

func (c *CsvSink) Open(ctx context.Context) <-chan error {
	result := make(chan error)
	if err := c.init(ctx); err != nil {
		go func() {
			result <- err
		}()
		return result
	}

	go func() {
		defer func() {
			// flush remaining bits
			c.csvWriter.Flush()
			if e := c.csvWriter.Error(); e != nil {
				go func() {
					result <- fmt.Errorf("IO flush error: %s", e)
				}()
				return
			}

			// close file
			if c.file != nil {
				if e := c.file.Close(); e != nil {
					go func() {
						result <- fmt.Errorf("Unable to close file %s: %s", c.file.Name(), e)
					}()
					return
				}
			}
			close(result)
			c.log.Print("execution completed")
		}()

		for item := range c.input {
			data, ok := item.([]string)

			if !ok { // bad situation, fail fast
				msg := fmt.Sprintf("Expecting []string, got unexpected type %T", data)
				c.log.Print(msg)
				panic(msg)
			}

			if e := c.csvWriter.Write(data); e != nil {
				//TODO distinguish error values for better handling
				perr := fmt.Errorf("Unable to write record to file: %s ", e)
				c.log.Print(perr)
				continue
			}

			// flush to io
			c.csvWriter.Flush()
			if e := c.csvWriter.Error(); e != nil {
				perr := fmt.Errorf("IO flush error: %s", e)
				c.log.Print(perr)
			}

			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()

	return result
}
