// Build explicitly with Apple's compiler; no Keychain access during build.
import Foundation
import Security
import LocalAuthentication
if CommandLine.arguments.count==2 && CommandLine.arguments[1]=="version" {FileHandle.standardOutput.write(Data("workkit-keychain-helper/v1".utf8));exit(0)}
let input = FileHandle.standardInput.readData(ofLength: 32769)
func fail() -> Never { exit(5) }
guard CommandLine.arguments.count == 2, CommandLine.arguments[1] == "exchange", input.count <= 32768,
      let r = try? JSONSerialization.jsonObject(with: input) as? [String:String],
      Set(r.keys) == Set(["operation","service","key","data"]),
      let op=r["operation"], let service=r["service"], let key=r["key"],
      service.hasPrefix("org.workkit."), key.count == 64,
      key.allSatisfy({"0123456789abcdef".contains($0)}) else { fail() }
let authContext=LAContext()
authContext.interactionNotAllowed=true
var query: [String:Any] = [kSecClass as String:kSecClassGenericPassword,
 kSecAttrService as String:service,kSecAttrAccount as String:key,
 kSecUseAuthenticationContext as String:authContext]
var found = false
var payload = Data()
if op == "get" {
 query[kSecReturnData as String]=true
 query[kSecMatchLimit as String]=kSecMatchLimitOne
 var result: CFTypeRef?
 let status=SecItemCopyMatching(query as CFDictionary,&result)
 if status == errSecSuccess { guard let data=result as? Data, data.count<=16384 else {fail()}; payload=data; found=true }
 else if status != errSecItemNotFound {fail()}
} else if op == "set" {
 guard let data=Data(base64Encoded:r["data"] ?? ""), data.count<=16384 else {fail()}
 let status=SecItemUpdate(query as CFDictionary,[kSecValueData as String:data] as CFDictionary)
 if status == errSecItemNotFound {query[kSecValueData as String]=data;guard SecItemAdd(query as CFDictionary,nil)==errSecSuccess else {fail()}}
 else if status != errSecSuccess {fail()}
 found=true
} else {fail()}
guard let output=try? JSONSerialization.data(withJSONObject:["found":found,"data":payload.base64EncodedString()]) else {fail()}
FileHandle.standardOutput.write(output)
