package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
)

// Start container and return container id
func startDockerContainer(img string) (string, *exec.Cmd, io.WriteCloser, error) {
	nameB := make([]byte, 8)
	_, err := rand.Read(nameB)
	if err != nil {
		return "", nil, nil, err
	}
	name := hex.EncodeToString(nameB)

	cmd := exec.Command("docker", "run", "--rm", "-i", "--entrypoint", "/bin/sh", "--name", name, img)
	in, err := cmd.StdinPipe() // get pipe else we'll close prematurely
	err = cmd.Start()
	if err != nil {
		return "", nil, nil, err
	}

	return name, cmd, in, nil
}

// Run raw job in container, stdout only
func runRawJobInContainer(container string, args ...string) ([]byte, error) {
	log.Println("Running...", args)
	return exec.Command("docker", append([]string{"exec", container}, args...)...).Output()
}

// Run job in container, collect stderr + out and convert to string
func runJobInContainer(container string, args ...string) (string, error) {
	rv, err := exec.Command("docker", append([]string{"exec", container}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(rv)), err
}

// Copy from one to the other, p2 is dir. Return the new path name
func copyFromTo(c1, c2 string, p1, p2 string) (string, error) {
	log.Println("Copying ", p1, " to ", p2)

	data, err := exec.Command("docker", "exec", c1, "cat", p1).Output()
	if err != nil {
		return "", err
	}

	destPath := path.Join(p2, path.Base(p1))
	cmd := exec.Command("docker", "exec", "-i", c2, "tee", destPath)
	in, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	err = cmd.Start()
	if err != nil {
		return "", err
	}
	_, err = io.Copy(in, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	err = in.Close()
	if err != nil {
		return "", err
	}

	err = cmd.Wait()
	if err != nil {
		return "", err
	}

	return destPath, nil
}

func lddIt(result string) map[string]string {
	rv := make(map[string]string)
	for _, l := range strings.Split(result, "\n") {
		split := strings.Index(l, " => ")
		if split >= 0 {
			values := strings.Split(l[split+4:], " ")
			if len(values) > 0 {
				rv[strings.TrimSpace(l[:split])] = strings.TrimSpace(values[0])
			}
		}
	}
	return rv
}

func main() {
	var sourceImage string
	var destImage string
	var binariesComma string

	flag.StringVar(&sourceImage, "source", "", "REQUIRED name of source image")
	flag.StringVar(&destImage, "dest", "", "REQUIRED name of destination image")
	flag.StringVar(&binariesComma, "binaries", "", "REQUIRED comma-separated binary names to process")
	flag.Parse()

	if sourceImage == "" {
		log.Fatal("must specify source image name")
	}
	if destImage == "" {
		log.Fatal("must specify destination image name")
	}
	if binariesComma == "" {
		log.Fatal("must specify at least one binary")
	}

	binaries := strings.Split(binariesComma, ",")
	dirName := "/tmp/magiclibs"

	// Start our disposable containers
	sourceID, sourceJob, sourceIn, err := startDockerContainer(sourceImage)
	if err != nil {
		log.Fatal(err)
	}
	defer sourceJob.Wait()
	defer sourceIn.Close()

	destID, destJob, destIn, err := startDockerContainer(destImage)
	if err != nil {
		log.Fatal(err)
	}
	defer destJob.Wait()
	defer destIn.Close()

	// Give a few seconds to startup
	time.Sleep(5 * time.Second)

	// Intialize our dir
	z, err := runJobInContainer(destID, "mkdir", "-p", dirName)
	if err != nil {
		log.Fatal(err, z)
	}

	// Now, for each binary
	for _, binaryName := range binaries {
		log.Println("Processing...", binaryName)

		path, err := runJobInContainer(sourceID, "which", binaryName)
		if err != nil {
			log.Fatal("Cannot find binary", err, path)
		}

		log.Println("Bin path:", path)

		newPath, err := copyFromTo(sourceID, destID, path, dirName)
		if err != nil {
			log.Fatal("Cannot find binary", err, path)
		}

		log.Println("New path:", newPath)
		result, err := runJobInContainer(destID, "chmod", "a+x", newPath)
		if err != nil {
			log.Fatal("Cannot make executable", err, result)
		}

		lddRaw, err := runJobInContainer(sourceID, "ldd", path)
		if err != nil {
			log.Fatal("Cannot make ldd raw", err, lddRaw)
		}
		lddOld := lddIt(lddRaw)

		done := false
		for !done {
			lddNewRaw, err := runJobInContainer(destID, "bash", "-c", fmt.Sprintf("LD_LIBRARY_PATH=%s ldd %s", dirName, newPath))
			if err != nil {
				log.Fatal("Cannot make ldd raw", err, lddNewRaw)
			}
			lddNew := lddIt(lddNewRaw)

			dirty := false
			for k, v := range lddNew {
				if v == "not" { // not found
					oldLibPath, ok := lddOld[k]
					if !ok {
						log.Fatal("Cannot find library on source:", k)
					}
					newLibPath, err := copyFromTo(sourceID, destID, oldLibPath, dirName)
					if err != nil {
						log.Fatal("Cannot find binary", err, newLibPath)
					}

					dirty = true
				}
			}

			if !dirty {
				done = true
			}
		}

		log.Println("Finished copying libs, trying job:")
		result, err = runJobInContainer(destID, "bash", "-c", fmt.Sprintf("LD_LIBRARY_PATH=%s %s --invalid-arg-so-crash", dirName, newPath))
		log.Println(result, err)
	}

	// Zip up result
	finalTar, err := runRawJobInContainer(destID, "tar", "-C", dirName, "-zcv", ".")
	if err != nil {
		log.Fatal(err)
	}

	// Write it out
	os.Stdout.Write(finalTar)
}
