package main

import (
	"crypto/sha256"
	"encoding/binary"
	"flag"
	"fmt"
	"math/big"
	"os"
	"runtime/pprof"
	"strconv"

	"ekyu.moe/cryptonight"
	"ekyu.moe/cryptonight/groestl"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/sha3"
	"lukechampine.com/blake3"

	"github.com/aead/skein/skein256"
	quark "github.com/catcoin-project/goquarkhash"
	x11 "gitlab.com/nitya-sattva/go-x11"
	"golang.org/x/crypto/scrypt"
)

var (
	algo             string
	psz              string
	coins            uint64
	pubkey           string
	timestamp, nonce uint
	bits             string
	profile          string
	threads          int
)

func init() {
	flag.StringVar(&algo, "algo", "sha256d", "Algo to use: sha256d, blake, blake3d (aka cathash, meow), blake2b, sha3, keccak, scrypt, x11, quark, cryptonight (v1,v2,v3/R), groestl, skein")
	flag.StringVar(&psz, "psz", "The Times 03/Jan/2009 Chancellor on brink of second bailout for banks", "pszTimestamp")
	flag.Uint64Var(&coins, "coins", uint64(50*100000000), "Number of coins")
	flag.StringVar(&pubkey, "pubkey", "04678afdb0fe5548271967f1a67130b7105cd6a828e03909a67962e0ea1f61deb649f6bc3f4cef38c4f35504e51ec112de5c384df7ba0b8d578a4c702b6bf11d5f", "Pubkey (required)")
	flag.UintVar(&timestamp, "timestamp", 1231006505, "Timestamp to use")
	flag.UintVar(&nonce, "nonce", 2083236893, "Nonce value")
	flag.StringVar(&bits, "bits", "1d00ffff", "Bits")
	flag.StringVar(&profile, "profile", "", "Write profile information into file (debug)")
	flag.IntVar(&threads, "threads", 4, "Number of threads to use")
}

func ComputeSha256(content []byte) []byte {
	m := sha256.New()
	m.Write(content)

	return m.Sum(nil)
}

// Fixed old problems. Wrong parameters etc.

func ComputeCryptonight(content []byte, variant int) []byte {
	return cryptonight.Sum(content, variant)
}

func ComputeGroestl(content []byte) []byte {
	h := groestl.New256()
	h.Write(content)
	return h.Sum(nil)
}

func ComputeSkein(content []byte) []byte {
	h := skein256.New256(nil)
	h.Write(content)
	return h.Sum(nil)
}

func ComputeScrypt(content []byte) []byte {
	scryptHash, err := scrypt.Key(content, content, 1024, 1, 1, 32)

	if err != nil {
		panic(err)
	}

	return scryptHash
}

func ComputeSHA3(content []byte) []byte {
	h := sha3.New256()
	h.Write(content)
	return h.Sum(nil)
}

func ComputeKeccak(content []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(content)
	return h.Sum(nil)
}

func ComputeBlake2b(content []byte) []byte {
	h, _ := blake2b.New256(nil)
	h.Write(content)
	return h.Sum(nil)
}

func ComputeBlake3(content []byte) []byte {
	hasher := blake3.New(32, nil)
	hasher.Write(content)
	return hasher.Sum(nil)
}

func ComputeX11(content []byte) []byte {
	out := make([]byte, 32)

	hasher := x11.New()
	hasher.Hash(content, out)

	return out
}

func ComputeQuark(content []byte) []byte {
	return quark.QuarkHash(content)
}

func Reverse(in []byte) []byte {
	out := make([]byte, len(in))

	for i := 0; i < len(in); i++ {
		out[i] = in[len(in)-i-1]
	}

	return out
}

type GenesisParams struct {
	Algo      string
	Psz       string
	Coins     uint64
	Pubkey    string
	Timestamp uint32
	Nonce     uint32
	Bits      uint32
}

func ComputeTarget(bits uint32) big.Int {
	var target big.Int

	target_bytes := make([]byte, bits>>24)
	binary.BigEndian.PutUint32(target_bytes, uint32(bits%(1<<24)<<8))

	target.SetBytes(target_bytes)

	return target
}

type Job struct {
	StartingNonce uint32
	MaxNonce      uint32
	Timestamp     uint32
}

func main() {
	flag.Parse()

	if profile != "" {
		f, err := os.Create(profile)
		if err != nil {
			panic(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	if psz == "" {
		fmt.Printf("Require a psz. Please set -psz")
		os.Exit(1)
	}

	jobs_num := threads
	jobs := make(chan Job, jobs_num)
	results := make(chan bool, jobs_num)

	for i := 0; i < jobs_num; i++ {
		go SearchWorker(jobs, results)
	}

	nonce_it := uint(1000)

	for {
		var res bool
		if jobs_num > 0 {
			jobs <- Job{
				StartingNonce: uint32(nonce),
				MaxNonce:      uint32(nonce + nonce_it),
				Timestamp:     uint32(timestamp),
			}
			new_nonce := nonce + nonce_it
			if new_nonce < nonce {
				timestamp++
			}
			nonce = new_nonce

			jobs_num--
		} else if jobs_num == 0 {
			// Wait for a job to be completed
			res = <-results
			jobs_num++
		}

		if res {
			break
		}
	}
}

func SearchWorker(jobs <-chan Job, results chan<- bool) {
	var hash []byte
	var current big.Int
	var found bool

	bits_uint32, err := strconv.ParseUint(bits, 16, 32)
	if err != nil {
		panic(err)
	}

	for job := range jobs {
		params := new(GenesisParams)
		params.Algo = algo
		params.Psz = psz
		params.Coins = coins
		params.Pubkey = pubkey
		params.Timestamp = job.Timestamp
		params.Nonce = job.StartingNonce
		params.Bits = uint32(bits_uint32)

		blk := CreateBlock(params)
		target := ComputeTarget(blk.Bits)

		for {
			switch params.Algo {
			case "sha256d":
				hash = ComputeSha256(ComputeSha256(blk.Serialize()))
				blk.Hash = hash
			case "blake3d":
				hash = ComputeBlake3(ComputeBlake3(blk.Serialize()))
				blk.Hash = hash
			case "cathash":
				hash = ComputeBlake3(ComputeBlake3(blk.Serialize()))
				blk.Hash = hash
			case "meow":
				hash = ComputeBlake3(ComputeBlake3(blk.Serialize()))
				blk.Hash = hash
			case "blake3":
				hash = ComputeBlake3(blk.Serialize())
				blk.Hash = hash
			case "sha3":
				hash = ComputeSHA3(blk.Serialize())
				blk.Hash = hash
			case "keccak":
				hash = ComputeKeccak(blk.Serialize())
				blk.Hash = hash
			case "blake2b":
				hash = ComputeBlake2b(blk.Serialize())
				blk.Hash = hash
			case "scrypt":
				hash = ComputeScrypt(blk.Serialize())
			case "x11":
				hash = ComputeX11(blk.Serialize())
				blk.Hash = hash
			case "quark":
				hash = quark.QuarkHash(blk.Serialize())
				blk.Hash = hash
			case "groestl":
				hash = ComputeGroestl(blk.Serialize())
				blk.Hash = hash
			case "cryptonightv1":
				hash = ComputeCryptonight(blk.Serialize(), 1)
				blk.Hash = hash
			case "cryptonightv2":
				hash = ComputeCryptonight(blk.Serialize(), 2)
				blk.Hash = hash
			case "cryptonightR":
				hash = ComputeCryptonight(blk.Serialize(), 3)
				blk.Hash = hash
			case "cryptonightv3":
				hash = ComputeCryptonight(blk.Serialize(), 3)
				blk.Hash = hash
			case "skein":
				hash = ComputeSkein(blk.Serialize())
				blk.Hash = hash
			}

			current.SetBytes(Reverse(hash))
			if 1 == target.Cmp(&current) {
				found = true
				PrintFound(blk, hash, target)
				break
			}

			blk.Nonce++
			if blk.Nonce == job.MaxNonce {
				break
			}
		}

		if found == true {
			results <- true
			break
		}

		results <- false
	}

}

func PrintFound(blk *Block, hash []byte, target big.Int) {
	fmt.Printf("Ctrl Hash:\t0x%x\n", Reverse(hash))
	target_hash := make([]byte, 32)
	copy(target_hash[32-len(target.Bytes()):], target.Bytes())
	fmt.Printf("Target:\t\t0x%x\n", target_hash)
	fmt.Printf("Blk Hash:\t0x%x\n", Reverse(blk.Hash))
	fmt.Printf("Mkl Hash:\t0x%x\n", Reverse(blk.MerkleRoot))
	fmt.Printf("Nonce:\t\t%d\n", blk.Nonce)
	fmt.Printf("Timestamp:\t%d\n", blk.Timestamp)
	fmt.Printf("Pubkey:\t\t%s\n", pubkey)
	fmt.Printf("Coins:\t\t%d\n", coins)
	fmt.Printf("Psz:\t\t'%s'\n", psz)
}
