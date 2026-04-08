package main

import (
	"fmt"
	"time"

	logger "github.com/solumath/dahua-plate-logger/internal"
)

func main() {
	store := logger.NewStore("plates")
	defer store.Close()

	cameras := []string{"camera_157", "camera_160"}
	plates := []string{"1AB2345", "9P08278", "ABC1234", "XY99ZZZ", "7K55DEF", "2BC4567", "PL8E123", "3TT0987"}

	loc := time.Now().Location()
	count := 0
	// ~15 events per day, randomish spacing across April 2026
	intervals := []int{37, 52, 18, 74, 29, 63, 41, 85, 22, 57, 33, 91, 47, 66, 25}
	for day := 1; day <= 30; day++ {
		minuteOffset := 0
		for j, gap := range intervals {
			minuteOffset += gap
			ts := time.Date(2026, 4, day, 0, minuteOffset, 0, 0, loc)
			e := logger.Event{
				Plate:  plates[(day+j)%len(plates)],
				Camera: cameras[(day+j)%len(cameras)],
				UTC:    ts.Unix(),
				UTCMS:  0,
			}
			if err := store.Insert(e); err != nil {
				fmt.Printf("day %d event %d: %v\n", day, j, err)
			} else {
				count++
			}
		}
	}
	fmt.Printf("inserted %d records\n", count)
}
