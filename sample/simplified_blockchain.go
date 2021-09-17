package main;

import(
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
	"github.com/joho/godotenv"
	"github.com/davecgh/go-spew/spew"
	"strings"
)

type Block struct {
	Index     int
	Merkleroot string
	Hash string
	PrevHash  string
	Validator string
}

// Blockchain is a series of validated Blocks
var Blockchain []Block
var tempBlocks []Block

// candidateBlocks handles incoming blocks for validation
var candidateBlocks = make(chan Block)

// announcements broadcasts winning validator to all nodes
var announcements = make(chan string)

var mutex = &sync.Mutex{}

// validators keeps track of open validators and balances
var validators = make(map[string]int)

func calculateHash(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	hashed := h.Sum(nil)
	return hex.EncodeToString(hashed)
}

//calculateBlockHash returns the hash of all block information
func calculateBlockHash(block Block) string {
	record := string(block.Index) + block.Merkleroot + block.PrevHash + block.Validator;
	return calculateHash(record)
}

//assume transactions to be strings.
func generateMerkleroot(transactions []string) string {
	var HashTotal string;
	for  i := 0; i < len(transactions); i++ {
		HashTotal += calculateHash(transactions[i]);
	}
	HashTotal = calculateHash(HashTotal);
	return HashTotal;
}

// generateBlock creates a new block using previous block's hash
func generateBlock(oldBlock Block, transactions []string, address string) (Block, error) {

	var newBlock Block

	newBlock.Index = oldBlock.Index + 1
	newBlock.Merkleroot = generateMerkleroot(transactions);
	newBlock.PrevHash = oldBlock.Hash
	newBlock.Validator = address
	newBlock.Hash = calculateBlockHash(newBlock)
	
	return newBlock, nil
}


func isBlockValid(newBlock, oldBlock Block, conn net.Conn) bool {
	if oldBlock.Index+1 != newBlock.Index {
		io.WriteString(conn, "index incorrect");
		return false
	}

	if oldBlock.Hash != newBlock.PrevHash {
		io.WriteString(conn, "hash incorrect");
		return false
	}

	if calculateBlockHash(newBlock) != newBlock.Hash {
		io.WriteString(conn, "new hash incorrect");
		return false
	}

	return true
}


func handleConn(conn net.Conn) {
	defer conn.Close()

	go func() {
		for {
			msg := <-announcements
			io.WriteString(conn, msg)
		}
	}()
	// validator address
	var address string

	// allow user to allocate number of tokens to stake
	// the greater the number of tokens, the greater chance to forging a new block
	io.WriteString(conn, "Enter token balance:")
	scanBalance := bufio.NewScanner(conn)
	for scanBalance.Scan() {
		balance, err := strconv.Atoi(scanBalance.Text())
		if err != nil {
			log.Printf("%v not a number: %v", scanBalance.Text(), err)
			return
		}
		t := time.Now()
		address = calculateHash(t.String())
		validators[address] = balance
		fmt.Println(validators)
		break
	}

	io.WriteString(conn, "\nEnter transaction strings")

	readtransactions := bufio.NewScanner(conn)

	go func() {
		for {
			// take in transactions from stdin and add it to blockchain after conducting necessary validation
			for readtransactions.Scan() {
				transactions := readtransactions.Text();
				io.WriteString(conn, transactions);
				var transactionsSplit = strings.Split(transactions, " ");
				fmt.Println(transactionsSplit, len(transactionsSplit));

				mutex.Lock()
				oldLastIndex := Blockchain[len(Blockchain)-1]
				mutex.Unlock()

				// create newBlock for consideration to be forged
				newBlock, err := generateBlock(oldLastIndex, transactionsSplit, address)
				if err != nil {
					log.Println(err)
					continue
				}
				if isBlockValid(newBlock, oldLastIndex, conn) {
					candidateBlocks <- newBlock
				}

				io.WriteString(conn, "\nEnter transaction strings")
			}
		}
	}()

	// main thread.
	for {
		time.Sleep(time.Minute)
		mutex.Lock()
		output, err := json.Marshal(Blockchain)
		mutex.Unlock()
		if err != nil {
			log.Fatal(err)
		}
		io.WriteString(conn, string(output)+"\n")
	}

}


func pickWinner() {
	time.Sleep(30 * time.Second)
	mutex.Lock()
	temp := tempBlocks
	mutex.Unlock()

	lotteryPool := []string{}
	if len(temp) > 0 {

	OUTER:
		for _, block := range temp {
			// if already in lottery pool, skip
			for _, node := range lotteryPool {
				if block.Validator == node {
					continue OUTER
				}
			}

			// lock list of validators to prevent data race
			mutex.Lock()
			setValidators := validators
			mutex.Unlock()

			k, ok := setValidators[block.Validator]
			if ok {
				for i := 0; i < k; i++ {
					lotteryPool = append(lotteryPool, block.Validator)
				}
			}
		}

		// randomly pick winner from lottery pool
		s := rand.NewSource(time.Now().Unix())
		r := rand.New(s)
		lotteryWinner := lotteryPool[r.Intn(len(lotteryPool))]

		// add block of winner to blockchain and let all the other nodes know
		for _, block := range temp {
			if block.Validator == lotteryWinner {
				mutex.Lock()
				Blockchain = append(Blockchain, block)
				mutex.Unlock()
				for _ = range validators {
					announcements <- "\nwinning validator: " + lotteryWinner + "\n"
				}
				break
			}
		}
	}

	mutex.Lock()
	tempBlocks = []Block{}
	mutex.Unlock()
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal(err)
	}

	// create genesis block
	genesisBlock := Block{}
	genesisBlock = Block{0, "0x0", calculateBlockHash(genesisBlock), "", ""}
	spew.Dump(genesisBlock)
	Blockchain = append(Blockchain, genesisBlock)

	// start TCP and serve TCP server
	server, err := net.Listen("tcp", ":"+os.Getenv("ADDR"))
	if err != nil {
		log.Fatal(err)
	}
	defer server.Close()

	go func() {
		for candidate := range candidateBlocks {
			mutex.Lock()
			tempBlocks = append(tempBlocks, candidate)
			mutex.Unlock()
		}
	}()

	go func() {
		for {
			pickWinner()
		}
	}()

	for {
		conn, err := server.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go handleConn(conn)
	}
}
