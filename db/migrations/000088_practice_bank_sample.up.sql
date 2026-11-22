-- Bank paket "Coba Halaman Ujian" (T161): ditandai sebagai bank contoh dan
-- diberi nama baru, hanya bila namanya belum diubah sekolah. Bank dikenali
-- dari soal yang dipakai ujian bawaan.
UPDATE exam_banks b SET sample = true,
       name = CASE WHEN b.name = 'Latihan Antarmuka (contoh)' THEN 'Contoh — Coba Halaman Ujian' ELSE b.name END
 WHERE EXISTS (SELECT 1 FROM exam_items i
                 JOIN exams e ON e.school_id = i.school_id AND e.id = i.exam_id AND e.builtin
                 JOIN exam_questions q ON q.school_id = i.school_id AND q.id = i.question_id
                WHERE q.school_id = b.school_id AND q.bank_id = b.id);
