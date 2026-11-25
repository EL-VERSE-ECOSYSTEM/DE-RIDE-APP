# Nigeria geocoding services

This stack provides Photon search and Nominatim search/reverse geocoding behind Caddy HTTPS. It is a self-hosted VPS deployment, separate from Render. It uses Nominatim's Nigeria Geofabrik extract and imports Nigeria into Photon on first startup. The first import can take hours and needs substantial free SSD space and memory; size the VPS for the Nigeria extract and both indexes, and monitor it during import. Do not treat the old `tonsnoei/photon-geocoder` `REGION=nigeria` example as a supported configuration: this stack builds the official Photon project and imports a country-filtered index.

## Start on an Ubuntu VPS

1. Point a DNS `A` record for a geocoder subdomain to the VPS public IP. Allow inbound TCP 80 and 443; keep ports 8080, 2322, and 5432 private.
2. Install Docker Engine and the Docker Compose plugin using Docker's official Ubuntu instructions. Set the host's `vm.max_map_count` to `262144` for Photon's embedded OpenSearch process.
3. In this directory, copy `.env.example` to `.env`. Set `GEOCODER_DOMAIN` to the DNS name and `FRONTEND_ORIGIN` to the exact HTTPS frontend origin. Set `NOMINATIM_PASSWORD` to a long randomly generated secret.
4. Start and follow the initial import:

   ```sh
   docker compose up -d --build
   docker compose logs -f nominatim photon
   ```

5. Once startup finishes, check `https://<GEOCODER_DOMAIN>/photon/api/?q=Lagos&limit=1` and `https://<GEOCODER_DOMAIN>/nominatim/search?q=Lagos&format=jsonv2`.

The Compose file persists Nominatim's database, flatnode data, Photon's index, and Caddy certificates in named volumes. Nominatim updates are disabled because Photon imports directly from its database and the initial import must not race with replication updates. To refresh geodata, plan a coordinated offline reimport into Photon after updating Nominatim; do not enable continuous updates without configuring Photon's supported update workflow. Back up the named volumes and test restores.

## Frontend settings

Set this server-side environment variable for the frontend project (for example, Vercel) and in `frontend/.env.local` when developing against your VPS:

```env
GEOCODER_DOMAIN=<GEOCODER_DOMAIN>
```

Use the host alone or an `https://` URL. The app's same-origin API routes build the Photon and Nominatim URLs from this value, so it must match the VPS DNS name. If the variable is blank or unset, the routes fall back to the public Photon and Nominatim APIs. Caddy adds CORS for only the configured `FRONTEND_ORIGIN`. Do not copy `NOMINATIM_PASSWORD` from the VPS `.env` into the frontend environment.

See the official [Photon usage guide](https://github.com/komoot/photon/blob/master/docs/usage.md) and [Nominatim Docker guide](https://github.com/mediagis/nominatim-docker/blob/master/howto.md) for index import and resource details. Country extracts and import times change; check available disk before starting.
