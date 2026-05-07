package weixin

import (
	"fmt"

	"github.com/skip2/go-qrcode"
)

// PrintQRCodeTerminal 在终端打印二维码
func PrintQRCodeTerminal(url string) error {
	// 生成二维码
	qr, err := qrcode.New(url, qrcode.Medium)
	if err != nil {
		return fmt.Errorf("generate qrcode: %w", err)
	}

	// 禁用边框，使终端显示更紧凑
	qr.DisableBorder = true

	// 获取二维码矩阵
	matrix := qr.Bitmap()

	// 打印顶部边框
	fmt.Println()
	printBorder(len(matrix)*2 + 2)

	// 打印二维码
	for _, row := range matrix {
		fmt.Print("│")
		for _, cell := range row {
			if cell {
				fmt.Print("██") // 黑色块
			} else {
				fmt.Print("  ") // 白色块
			}
		}
		fmt.Println("│")
	}

	// 打印底部边框
	printBorder(len(matrix)*2 + 2)
	fmt.Println()

	return nil
}

func printBorder(width int) {
	fmt.Print("┌")
	for i := 0; i < width; i++ {
		fmt.Print("─")
	}
	fmt.Println("┐")
}
