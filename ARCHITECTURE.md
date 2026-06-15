# 📸 Detailed Immich Optimization Pipeline Architecture

This document outlines the three-stage architecture for a self-hosted media backup and optimization system. This approach prioritizes **storage efficiency** and **data resilience** by decoupling transfer, processing, and cataloging.

---

## I. The System Components and Roles

| Component | Primary Role | Key Mechanism |
| :--- | :--- | :--- |
| **Mobile Client (FolderSync)** | **Secure Ingestion & Deduplication** | Manages one-way sync, retries, battery optimization, and network constraints (Wi-Fi/VPN). |
| **Network Server (SFTP/SMB/FTP)** | **Landing Zone Repository** | Provides the network service (SFTP, SMB, etc.) for secure file delivery to the **watched folder**. |
| **Immich Optimizer** | **Transactional Processing Engine** | Compresses original files, uploads the smaller version to Immich, and handles the atomic cleanup/failover. |
| **Immich** | **Final Catalog & Indexing** | Stores the *optimized* file, generates thumbnails, extracts metadata, and provides the user interface. |

---

## II. Stage 1: Secure Ingestion and Atomic Placement

This stage moves the original media from the mobile device to a secured location on the server's local disk, creating the initial **safety buffer**.

### 1. Transfer Protocol Selection and Setup

| Protocol | Security Note | Use Case & Server Setup Example |
| :--- | :--- | :--- |
| **SFTP** | **Encrypted** (via SSH). **Recommended.** | Best for transfers across the internet or untrusted networks. Uses Docker image like `atmoz/sftp`. |
| **SMB** | **Encrypted** (if v3+ is used). | Excellent performance for **Local Area Network (LAN) only** setups. Requires a host-based Samba service. |
| **FTP** | **Plaintext.** **Discouraged.** | Only acceptable on highly trusted, isolated internal networks due to sending credentials unencrypted. |

### 2. Client-Side Transfer Logic

1.  **Client Connection:** The **FolderSync** app initiates a connection using the chosen protocol (SFTP, SMB, etc.) and authenticates.
2.  **Deduplication Check:** FolderSync runs its **strict one-way sync** check, ensuring it only selects files not previously marked as successfully transferred.
3.  **Encrypted Data Transfer:** The original, high-resolution media file is transferred across the network to the server.
4.  **File Landing:** The network server container (e.g., SFTP) uses a **volume/bind mount** to write the original file directly into the **watched folder** on the host filesystem. The media is now secured on the server.

---

## III. Stage 2: Atomic Optimization and API Upload

The **Immich Optimizer** container monitors the mounted **watched folder** and executes a transactional loop to process the files.

### 1. Processing Loop and Atomicity

1.  **Polling and Detection:** The Optimizer runs a continuous loop, identifying a new, unprocessed **Original File** in the watched directory.
2.  **File Read & Optimization:** The Original File is read, and the compression utility is executed (e.g., converting a large JPEG to a smaller JPEG-XL or transcoding a video).
3.  **New File Creation:** A new **Optimized File** is created. This step is critical because the act of compression generates a file with a completely different **digital hash**.
4.  **Immich API Upload:** The Optimizer sends the smaller **Optimized File** to the Immich API endpoint.
5.  **Confirmation & Clean Up:** The Optimizer waits for an explicit success confirmation (HTTP `200 OK`) from the Immich API, verifying the file has been accepted and written to Immich's storage. Only then is the large **Original File deleted** from the watched folder.

### 2. Robust Failover Logic

* **Success:** Original file is deleted.
* **Optimization failure:** If optimization fails, the Original File is uploaded to Immich instead.
* **Upload failure:** If uploading the optimized file or original fallback fails, the source is copied to the **undone/** subfolder. This ensures **zero data loss** and reserves the file for manual troubleshooting.

---

## IV. Stage 3: Final Storage

**Permanent Storage:** The final, optimized version of the file is written to Immich, ready for access via all client applications.

---

## ✅ Architectural Resilience (Why this Setup is Best)

This pipeline's complexity is intentional, providing features essential for reliable self-hosting:

* **Client Resilience:** FolderSync handles automatic retries, respects connectivity rules (Wi-Fi/VPN), and bypasses mobile operating system limitations (like Android battery optimization) that often kill background uploads.
* **Server Safety Buffer:** The **watched folder** acts as a reliable safety buffer. If the **Immich** or **Optimizer** services are temporarily down for maintenance or updates, the mobile client still successfully offloads the files to the server's disk via SFTP/SMB. Your photos are **secured immediately** on the server's permanent storage, minimizing the risk of data loss compared to the official Immich app, which requires the entire server stack to be online for backup.
