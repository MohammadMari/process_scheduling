package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/olekukonko/tablewriter"
)

func main() {
	// CLI args
	f, closeFile, err := openProcessingFile(os.Args...)
	if err != nil {
		log.Fatal(err)
	}
	defer closeFile()

	// Load and parse processes
	processes, err := loadProcesses(f)
	if err != nil {
		log.Fatal(err)
	}

	// First-come, first-serve scheduling
	FCFSSchedule(os.Stdout, "First-come, first-serve", processes)
	SJFSchedule(os.Stdout, "Shortest-job-first", processes)
	SJFPrioritySchedule(os.Stdout, "Priority", processes)
	RRSchedule(os.Stdout, "Round-robin", processes)
}

func openProcessingFile(args ...string) (*os.File, func(), error) {
	if len(args) != 2 {
		return nil, nil, fmt.Errorf("%w: must give a scheduling file to process", ErrInvalidArgs)
	}
	// Read in CSV process CSV file
	f, err := os.Open(args[1])
	if err != nil {
		return nil, nil, fmt.Errorf("%v: error opening scheduling file", err)
	}
	closeFn := func() {
		if err := f.Close(); err != nil {
			log.Fatalf("%v: error closing scheduling file", err)
		}
	}

	return f, closeFn, nil
}

type (
	Process struct {
		ProcessID     int64
		ArrivalTime   int64
		BurstDuration int64
		Priority      int64
	}
	TimeSlice struct {
		PID   int64
		Start int64
		Stop  int64
	}
)

//region Schedulers

// FCFSSchedule outputs a schedule of processes in a GANTT chart and a table of timing given:
// • an output writer
// • a title for the chart
// • a slice of processes
func FCFSSchedule(w io.Writer, title string, processes []Process) {
	var (
		serviceTime     int64
		totalWait       float64
		totalTurnaround float64
		lastCompletion  float64
		waitingTime     int64
		schedule        = make([][]string, len(processes))
		gantt           = make([]TimeSlice, 0)
	)
	for i := range processes {
		if processes[i].ArrivalTime > 0 {
			waitingTime = serviceTime - processes[i].ArrivalTime
		}
		totalWait += float64(waitingTime)

		start := waitingTime + processes[i].ArrivalTime

		turnaround := processes[i].BurstDuration + waitingTime
		totalTurnaround += float64(turnaround)

		completion := processes[i].BurstDuration + processes[i].ArrivalTime + waitingTime
		lastCompletion = float64(completion)

		schedule[i] = []string{
			fmt.Sprint(processes[i].ProcessID),
			fmt.Sprint(processes[i].Priority),
			fmt.Sprint(processes[i].BurstDuration),
			fmt.Sprint(processes[i].ArrivalTime),
			fmt.Sprint(waitingTime),
			fmt.Sprint(turnaround),
			fmt.Sprint(completion),
		}
		serviceTime += processes[i].BurstDuration

		gantt = append(gantt, TimeSlice{
			PID:   processes[i].ProcessID,
			Start: start,
			Stop:  serviceTime,
		})
	}

	count := float64(len(processes))
	aveWait := totalWait / count
	aveTurnaround := totalTurnaround / count
	aveThroughput := count / lastCompletion

	outputTitle(w, title)
	outputGantt(w, gantt)
	outputSchedule(w, schedule, aveWait, aveTurnaround, aveThroughput)
}

func SJFPrioritySchedule(w io.Writer, title string, processes []Process) {
	var (
		totalBurstTime   int
		totalWait        float64
		totalTurnaround  float64
		lastCompletion   float64
		processBurstLeft = make([]int, len(processes))
		schedule         = make([][]string, len(processes))
		gantt            = make([]TimeSlice, 0)
	)

	for i := range processes {
		totalBurstTime += int(processes[i].BurstDuration)
		processBurstLeft[i] = int(processes[i].BurstDuration)
	}

	lastGantIndex := -1
	lastGantStartTime := 0
	for timestep := 0; timestep < totalBurstTime; timestep++ {
		leastJobIndex := -1
		leastJobBurstTime := 100000000 // INT_MAX
		leastJobPriority := 100000000  // INT_MAX

		// find shortest current process
		for i := range processes {
			// make sure there is work left to be done
			if processBurstLeft[i] <= 0 {
				continue
			}

			// make sure the process has arrived
			if processes[i].ArrivalTime > int64(timestep) {
				continue
			}

			if processes[i].Priority > int64(leastJobPriority) {
				continue
			}

			// lowest priority? becomes our best
			if processes[i].Priority < int64(leastJobPriority) {
				leastJobPriority = int(processes[i].Priority)
				leastJobBurstTime = int(processes[i].BurstDuration)
				leastJobIndex = i
				continue
			}

			// shortest job?
			if processBurstLeft[i] >= leastJobBurstTime {
				continue
			}

			leastJobIndex = i
			leastJobBurstTime = processBurstLeft[i]
			leastJobPriority = int(processes[i].Priority)
		}

		if leastJobIndex == -1 {
			totalBurstTime++
			continue
		}

		if lastGantIndex != leastJobIndex || timestep == totalBurstTime-1 {
			if lastGantIndex != -1 {
				gantt = append(gantt, TimeSlice{
					PID:   processes[lastGantIndex].ProcessID,
					Start: int64(lastGantStartTime),
					Stop:  int64(timestep + 1),
				})
			}

			lastGantStartTime = timestep
			lastGantIndex = leastJobIndex
		}

		processBurstLeft[leastJobIndex]--
		// is job done?
		if processBurstLeft[leastJobIndex] == 0 {
			totalTurnaround += float64(int64(timestep+1) - processes[leastJobIndex].ArrivalTime)
			waitTime := float64((timestep + 1) - int(processes[leastJobIndex].ArrivalTime) - int(processes[leastJobIndex].BurstDuration))
			totalWait += waitTime

			schedule[leastJobIndex] = []string{
				fmt.Sprint(processes[leastJobIndex].ProcessID),
				fmt.Sprint(processes[leastJobIndex].Priority),
				fmt.Sprint(processes[leastJobIndex].BurstDuration),
				fmt.Sprint(processes[leastJobIndex].ArrivalTime),
				fmt.Sprint((timestep + 1) - int(processes[leastJobIndex].ArrivalTime) - int(processes[leastJobIndex].BurstDuration)),
				fmt.Sprint((timestep + 1) - int(processes[leastJobIndex].ArrivalTime)),
				fmt.Sprint(timestep + 1),
			}

			lastCompletion = float64(processes[leastJobIndex].BurstDuration + processes[leastJobIndex].ArrivalTime + int64(waitTime))
		}
	}

	count := float64(len(processes))
	outputTitle(w, title)
	outputGantt(w, gantt)
	outputSchedule(w, schedule, totalWait/count, totalTurnaround/count, count/lastCompletion)
}

func SJFSchedule(w io.Writer, title string, processes []Process) {
	var (
		totalBurstTime   int
		totalWait        float64
		totalTurnaround  float64
		lastCompletion   float64
		processBurstLeft = make([]int, len(processes))
		schedule         = make([][]string, len(processes))
		gantt            = make([]TimeSlice, 0)
	)

	for i := range processes {
		totalBurstTime += int(processes[i].BurstDuration)
		processBurstLeft[i] = int(processes[i].BurstDuration)
	}

	lastGantIndex := -1
	lastGantStartTime := 0
	for timestep := 0; timestep < totalBurstTime; timestep++ {
		leastJobIndex := -1
		leastJobBurstTime := 100000000 // INT_MAX

		// find shortest current process
		for i := range processes {
			// make sure there is work left to be done
			if processBurstLeft[i] <= 0 {
				continue
			}

			// make sure the process has arrived
			if processes[i].ArrivalTime > int64(timestep) {
				continue
			}

			// shortest job?
			if processBurstLeft[i] >= leastJobBurstTime {
				continue
			}

			leastJobIndex = i
			leastJobBurstTime = processBurstLeft[i]
		}

		if lastGantIndex != leastJobIndex || timestep == totalBurstTime-1 {
			if lastGantIndex != -1 {
				gantt = append(gantt, TimeSlice{
					PID:   processes[lastGantIndex].ProcessID,
					Start: int64(lastGantStartTime),
					Stop:  int64(timestep + 1),
				})
			}

			lastGantStartTime = timestep
			lastGantIndex = leastJobIndex
		}

		if leastJobIndex == -1 {
			totalBurstTime++
			continue
		}

		processBurstLeft[leastJobIndex]--
		// is job done?
		if processBurstLeft[leastJobIndex] == 0 {
			totalTurnaround += float64(int64(timestep+1) - processes[leastJobIndex].ArrivalTime)
			waitTime := float64((timestep + 1) - int(processes[leastJobIndex].ArrivalTime) - int(processes[leastJobIndex].BurstDuration))
			totalWait += waitTime

			schedule[leastJobIndex] = []string{
				fmt.Sprint(processes[leastJobIndex].ProcessID),
				fmt.Sprint(processes[leastJobIndex].Priority),
				fmt.Sprint(processes[leastJobIndex].BurstDuration),
				fmt.Sprint(processes[leastJobIndex].ArrivalTime),
				fmt.Sprint((timestep + 1) - int(processes[leastJobIndex].ArrivalTime) - int(processes[leastJobIndex].BurstDuration)),
				fmt.Sprint((timestep + 1) - int(processes[leastJobIndex].ArrivalTime)),
				fmt.Sprint(timestep + 1),
			}

			lastCompletion = float64(processes[leastJobIndex].BurstDuration + processes[leastJobIndex].ArrivalTime + int64(waitTime))
		}
	}

	count := float64(len(processes))
	outputTitle(w, title)
	outputGantt(w, gantt)
	outputSchedule(w, schedule, totalWait/count, totalTurnaround/count, count/lastCompletion)
}

func RRSchedule(w io.Writer, title string, processes []Process) {
	var (
		totalBurstTime   int
		totalWait        float64
		totalTurnaround  float64
		lastCompletion   float64
		processBurstInit = make([]int, len(processes))
		schedule         = make([][]string, len(processes))
		gantt            = make([]TimeSlice, 0)
	)

	for i := range processes {
		for j := i; j < len(processes); j++ {
			if processes[i].ArrivalTime > processes[j].ArrivalTime {
				processes[i], processes[j] = processes[j], processes[i]
			}
		}
	}

	for i := range processes {
		processBurstInit[i] = int(processes[i].BurstDuration)
		totalBurstTime += int(processes[i].BurstDuration)
	}

	for timestep := 0; timestep < totalBurstTime; {
		ranProcess := false
		for i := range processes {
			if processes[i].ArrivalTime > int64(timestep) {
				continue
			}

			if processes[i].BurstDuration <= 0 {
				continue
			}

			processes[i].BurstDuration--
			timestep++
			ranProcess = true

			gantt = append(gantt, TimeSlice{
				PID:   processes[i].ProcessID,
				Start: int64(timestep - 1),
				Stop:  int64(timestep),
			})

			if processes[i].BurstDuration == 0 {
				totalTurnaround += float64(int64(timestep+1) - processes[i].ArrivalTime)
				waitTime := float64((timestep) - int(processes[i].ArrivalTime) - int(processes[i].BurstDuration))
				totalWait += waitTime
				lastCompletion = float64(processes[i].BurstDuration + processes[i].ArrivalTime + int64(waitTime))

				schedule[i] = []string{
					fmt.Sprint(processes[i].ProcessID),
					fmt.Sprint(processes[i].Priority),
					fmt.Sprint(int(processBurstInit[i])),
					fmt.Sprint(processes[i].ArrivalTime),
					fmt.Sprint((timestep + 1) - int(processes[i].ArrivalTime) - int(processBurstInit[i])),
					fmt.Sprint((timestep + 1) - int(processes[i].ArrivalTime)),
					fmt.Sprint(timestep + 1),
				}
			}
		}

		if !ranProcess {
			timestep++
			totalBurstTime++
		}
	}

	count := float64(len(processes))
	outputTitle(w, title)
	outputGantt(w, gantt)
	outputSchedule(w, schedule, totalWait/count, totalTurnaround/count, count/lastCompletion)
}

//endregion

//region Output helpers

func outputTitle(w io.Writer, title string) {
	_, _ = fmt.Fprintln(w, strings.Repeat("-", len(title)*2))
	_, _ = fmt.Fprintln(w, strings.Repeat(" ", len(title)/2), title)
	_, _ = fmt.Fprintln(w, strings.Repeat("-", len(title)*2))
}

func outputGantt(w io.Writer, gantt []TimeSlice) {
	_, _ = fmt.Fprintln(w, "Gantt schedule")
	_, _ = fmt.Fprint(w, "|")
	for i := range gantt {
		pid := fmt.Sprint(gantt[i].PID)
		padding := strings.Repeat(" ", (8-len(pid))/2)
		_, _ = fmt.Fprint(w, padding, pid, padding, "|")
	}
	_, _ = fmt.Fprintln(w)
	for i := range gantt {
		_, _ = fmt.Fprint(w, fmt.Sprint(gantt[i].Start), "\t")
		if len(gantt)-1 == i {
			_, _ = fmt.Fprint(w, fmt.Sprint(gantt[i].Stop))
		}
	}
	_, _ = fmt.Fprintf(w, "\n\n")
}

func outputSchedule(w io.Writer, rows [][]string, wait, turnaround, throughput float64) {
	_, _ = fmt.Fprintln(w, "Schedule table")
	table := tablewriter.NewWriter(w)
	table.SetHeader([]string{"ID", "Priority", "Burst", "Arrival", "Wait", "Turnaround", "Exit"})
	table.AppendBulk(rows)
	table.SetFooter([]string{"", "", "", "",
		fmt.Sprintf("Average\n%.2f", wait),
		fmt.Sprintf("Average\n%.2f", turnaround),
		fmt.Sprintf("Throughput\n%.2f/t", throughput)})
	table.Render()
}

//endregion

//region Loading processes.

var ErrInvalidArgs = errors.New("invalid args")

func loadProcesses(r io.Reader) ([]Process, error) {
	rows, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%w: reading CSV", err)
	}

	processes := make([]Process, len(rows))
	for i := range rows {
		processes[i].ProcessID = mustStrToInt(rows[i][0])
		processes[i].BurstDuration = mustStrToInt(rows[i][1])
		processes[i].ArrivalTime = mustStrToInt(rows[i][2])
		if len(rows[i]) == 4 {
			processes[i].Priority = mustStrToInt(rows[i][3])
		}
	}

	return processes, nil
}

func mustStrToInt(s string) int64 {
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	return i
}

//endregion
