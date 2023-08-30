// This is an alternative implementation of the parslices implementation, where the synchronization
// for application of the sequential effects is done without deploying multiple go routines as in the
// `parslices.go` implementation. Here only `numThreads` go routines are spawned, and each apply all the effects.
// The synchronization so that a thread waits for the others to finish their effects application is done using
// waitGroups and counters to act like barriers and channels.
// This ended up having the same performance as the `parslices.go` implementation. Since `parslices.go` is easier 
// to understand, I kept it as the main implementation, but I'm keeping this script for reference.

package scheduler
import (
	"sync"
	"proj3/png"
	"proj3/utils"
	"proj3/mysync"
	"fmt"
	"time"
)

// Apply all effects in 'kernels to a slice of 'img'.
// 'worker'  waits for other workers to finish the application of an effect before proceeding to the next effect.
func worker(img *png.Image, slice ImageSlice, kernels []*png.Kernel, startWG *sync.WaitGroup,
	 endWG *sync.WaitGroup, imgWG *sync.WaitGroup, nWorkers int, tLock *mysync.TASLock, counter *int) {

	// loop: apply each effect in 'kernels' to the image slice
	for _, kernel := range kernels {
		
		// signal ready to start effect
		// fmt.Println("Thread ", mysync.GetGID(), "called startWG.Done()")
		startWG.Done()

		// wait until all are ready
		// fmt.Println("Thread ", mysync.GetGID(), "called startWG.wait()")
		startWG.Wait()
		
		// apply effect
		// fmt.Println("Thread ", mysync.GetGID(), "applied effect", i)
		img.ApplyEffectSlice2(kernel, slice.YStart, slice.YEnd, slice.XStart, slice.XEnd)

		// set waitGroup counter to 'nWorkers' for effects synchronization.
		// obs: only one worker will execute this in each iteration
		mysync.ExecuteOne(counter, tLock, nWorkers, func() {
			endWG.Add(nWorkers)
		})

		// signal effect application complete
		endWG.Done()		
		
		// wait until all threads are done with their slices before applying next effect
		endWG.Wait()

		// updates image buffer containing to apply the next effect (see png.Image struct definition)
		mysync.ExecuteOne(counter, tLock, nWorkers, func() {
			img.Final = 1 - img.Final
			// fmt.Println("Thread ", mysync.GetGID(), "reset start wait group")
			startWG.Add(nWorkers)
		})
	}
	// signal slice processing complete
	imgWG.Done()
}


// Process images specified by 'config' and 'effects.txt' dividing them into slices 
// and deploying 'config.ThreadCount' goroutines to apply effects to each slice. 
// Obs: Each image is loaded, processed and saved at a time.
func RunParallelSlices2(config Config) {
	//start timer
	startTime := time.Now()

	// create a queue of tasks given data directories CMD inputs and effects.txt file
	taskQueue := utils.CreateTasks(config.DataDirs)
	
	// compute number of threads to use
	nThreads := config.ThreadCount
	if nThreads > len(taskQueue.Tasks){
		nThreads = len(taskQueue.Tasks)
	}

	// Definition of elements used for syncronization:
	// imgWG waits until all threads are done with their slices of an image before proceeding to next image
	var imgWG sync.WaitGroup
	
	// auxiliar lock and counter to synchronize application of each effect by each goroutine
	tLock := mysync.NewTasLock()

	// counters to synchronize application of each effect by each goroutine
	counter := 0

	// placeholder for cumulative time of parallel tasks
	var totalParallelTime time.Duration

	// loop: load image from queue, divide into slices, deploy go routines to process each slice
	for i := 0; i < len(taskQueue.Tasks); i++ {
		// load the image
		img, _ := png.Load(taskQueue.Tasks[i].InPath)
		
		// create image slices
		slices := SlicesByRow(img, nThreads)
		
		// create slice of kernels representing each effect to be accessed by all threads
		kernels := png.CreateKernels(taskQueue.Tasks[i].Effects)
		
		// start timer for parallel section
		startParallel := time.Now()

		// effectWG helps synchronize application of each effect by each goroutine
		var endWG sync.WaitGroup
		counter = 0

		// use a wait group to synchronize the start of each effect application
		var startWG sync.WaitGroup
		startWG.Add(nThreads)

		// spawn a worker to process each slice 
		for _, slice := range slices {
			imgWG.Add(1)
			go  worker(img, slice, kernels, &startWG, &endWG, &imgWG, nThreads, &tLock, &counter)
		}
		// wait for all workers to finish their slices
		imgWG.Wait()

		// compute elapsed time for parallel section and accumulate
		totalParallelTime += time.Since(startParallel)
		
		// save processed image
		img.Save(taskQueue.Tasks[i].OutPath)
	}

	// compute total elapsed time
	elapsedTime := time.Since(startTime)

	// write result into JSON format 
	writeStr := fmt.Sprintf("{\"mode\": \"%s\", \"threads\": %d, \"timeElapsed\": %f, \"timeParallel\": %f , \"datadir\": \"%s\"}\n", 
								config.Mode ,nThreads, elapsedTime.Seconds(), totalParallelTime.Seconds(), config.DataDirs)
	// write elapsed time to a text file
	utils.WriteToFile(resultsPath, writeStr)
}
