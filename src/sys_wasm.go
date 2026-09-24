//go:build wasm
// +build wasm

package main

import (
	"bytes"
	"errors"
	"fmt"
	"syscall/js"
)

// webFileReader đóng vai trò như một file ảo trên RAM.
// Nó đáp ứng đủ điều kiện của một io.ReadSeekCloser để game có thể đọc và tua file.
type webFileReader struct {
	reader *bytes.Reader
}

func (w *webFileReader) Read(p []byte) (n int, err error) {
	return w.reader.Read(p)
}

func (w *webFileReader) Seek(offset int64, whence int) (int64, error) {
	return w.reader.Seek(offset, whence)
}

func (w *webFileReader) Close() error {
	// Thực ra file nằm trên RAM thì không cần đóng gì cả, nhưng cứ phải viết cho đúng chuẩn
	return nil
}

// Hàm fetchFromWeb dùng syscall/js để mượn chức năng Fetch của Trình duyệt tải file
func fetchFromWeb(filename string) ([]byte, error) {
	// 1. Chặn lại chờ tải xong. Vì Fetch của JS chạy ngầm (async), nên ta phải đợi
	ch := make(chan struct{})
	var fileBytes []byte
	var fetchErr error

	// 2. Mượn đối tượng window (Global) của trình duyệt
	global := js.Global()

	// 3. Chuẩn bị đường link để tải. (Giả sử file tĩnh được host cùng thư mục với trang web)
	// Bạn có thể cần điều chỉnh url này tùy theo cách bạn sắp xếp file trên Server Node.js
	url := "/" + filename

	// 4. Lệnh gọi Fetch API của JavaScript
	req := global.Call("fetch", url)

	// 5. Code xử lý khi tải thành công (Promise.then)
	req.Call("then", js.FuncOf(func(this js.Value, args []js.Value) any {
		resp := args[0]

		if !resp.Get("ok").Bool() { // Nếu Server trả về lỗi 404 (Không thấy file)
			fetchErr = fmt.Errorf("Lỗi 404: Không tải được file %s", url)
			close(ch)
			return nil
		}

		// Lấy data dạng ArrayBuffer (Mảng Byte gốc)
		resp.Call("arrayBuffer").Call("then", js.FuncOf(func(this js.Value, args2 []js.Value) any {
			buffer := args2[0]
			// Chuyển từ mảng Byte của JS sang mảng Byte của Golang
			uint8Array := global.Get("Uint8Array").New(buffer)
			fileBytes = make([]byte, uint8Array.Length())
			js.CopyBytesToGo(fileBytes, uint8Array)

			close(ch) // Báo hiệu đã tải xong
			return nil
		}))
		return nil
	})).Call("catch", js.FuncOf(func(this js.Value, args []js.Value) any {
		// Bắt lỗi nếu mạng bị rớt
		fetchErr = errors.New("Lỗi mạng khi tải file: " + url)
		close(ch)
		return nil
	}))

	<-ch // Tạm dừng game ở đây để đợi JS tải xong file

	if fetchErr != nil {
		return nil, fetchErr
	}

	return fileBytes, nil
}
