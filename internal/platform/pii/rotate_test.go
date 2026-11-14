package pii

import (
	"bytes"
	"context"
	"os"
	"testing"

	"akra/internal/platform/database"
)

func TestRotate(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE pii_rotate_test (id int PRIMARY KEY, nik_enc bytea, nik_idx bytea) ON COMMIT DROP`); err != nil {
		t.Fatal(err)
	}
	col := Column{Table: "pii_rotate_test", Key: "id", Column: "nik_enc", AADName: "pii_rotate_test.nik"}

	v1 := testKeyring(t, 1)
	niks := []string{"3201010101010001", "3201010101010002", "3201010101010003"}
	for i, nik := range niks {
		ct, _ := v1.Encrypt(nik, col.AADName)
		if _, err := tx.Exec(ctx, `INSERT INTO pii_rotate_test VALUES ($1, $2, $3)`, i+1, ct, v1.BlindIndex("nik", nik)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO pii_rotate_test VALUES (99, NULL, NULL)`); err != nil {
		t.Fatal(err)
	}

	// Nilai utuh tidak boleh tersimpan sebagai teks di database.
	var leaked bool
	if err := tx.QueryRow(ctx, `SELECT bool_or(position(convert_to($1, 'UTF8') in nik_enc) > 0) FROM pii_rotate_test`, niks[0]).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked {
		t.Fatal("plaintext NIK found in ciphertext column")
	}

	v12 := testKeyring(t, 1, 2)
	n, err := Rotate(ctx, tx, v12, col, 2) // batch kecil agar lebih dari satu putaran
	if err != nil || n != len(niks) {
		t.Fatalf("Rotate = %d, %v; want %d", n, err, len(niks))
	}
	if n, err := Rotate(ctx, tx, v12, col, 2); err != nil || n != 0 {
		t.Fatalf("second Rotate = %d, %v; want 0", n, err)
	}

	rows, err := tx.Query(ctx, `SELECT id, nik_enc, nik_idx FROM pii_rotate_test WHERE nik_enc IS NOT NULL ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var ct, idx []byte
		if err := rows.Scan(&id, &ct, &idx); err != nil {
			t.Fatal(err)
		}
		if ct[0] != 2 {
			t.Errorf("row %d still uses key version %d", id, ct[0])
		}
		got, err := v12.Decrypt(ct, col.AADName)
		if err != nil || got != niks[id-1] {
			t.Errorf("row %d = %q, %v", id, got, err)
		}
		if !bytes.Equal(idx, v12.BlindIndex("nik", got)) {
			t.Errorf("row %d blind index changed", id)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	// Kunci yang salah: gagal dengan error, bukan menulis data rusak.
	wrong := testKeyring(t, 3)
	wrong.aeads[2], _ = newAEAD(bytes.Repeat([]byte{0x55}, KeySize))
	if _, err := Rotate(ctx, tx, wrong, col, 10); err == nil {
		t.Error("Rotate with wrong key: error = nil")
	}

	if _, err := Rotate(ctx, tx, v12, Column{Table: "x; DROP TABLE y", Key: "id", Column: "c", AADName: "a"}, 1); err == nil {
		t.Error("invalid identifier accepted")
	}
}
