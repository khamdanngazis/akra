-- Keandalan dan observabilitas (T145): kejadian (alert) dan jadwal
-- pemeliharaan platform. Tabel platform, bukan milik sekolah.

-- Kejadian terbuka per jenis paling banyak satu: replika yang pertama
-- membuka kejadian yang mengirim alert (indeks unik parsial), sehingga
-- beberapa replika tidak mengirim alert ganda. Kejadian terbuka membuat
-- halaman status menampilkan "gangguan".
CREATE TABLE ops_incidents (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind        text        NOT NULL CHECK (kind ~ '^[a-z_]{1,40}$'),
    detail      text        NOT NULL DEFAULT '' CHECK (length(detail) <= 1000),
    opened_at   timestamptz NOT NULL DEFAULT now(),
    -- Terakhir kondisi masih buruk (dari replika mana pun).
    seen_at     timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    notified_at timestamptz
);
CREATE UNIQUE INDEX ops_incidents_open_key ON ops_incidents (kind) WHERE resolved_at IS NULL;
CREATE INDEX ops_incidents_opened_idx ON ops_incidents (opened_at DESC);

-- Pemeliharaan terjadwal: tampil di halaman status sejak 24 jam sebelum.
CREATE TABLE ops_maintenance (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    starts_at  timestamptz NOT NULL,
    ends_at    timestamptz NOT NULL,
    message    text        NOT NULL CHECK (btrim(message) <> '' AND length(message) <= 500),
    created_at timestamptz NOT NULL DEFAULT now(),
    cancelled_at timestamptz,
    CONSTRAINT ops_maintenance_window_check CHECK (ends_at > starts_at)
);
CREATE INDEX ops_maintenance_ends_idx ON ops_maintenance (ends_at) WHERE cancelled_at IS NULL;
