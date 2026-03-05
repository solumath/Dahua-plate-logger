Dim wmi, processes, process, count                                                                                                                                                           
count = 0                              
Set wmi = GetObject("winmgmts:")                                                                                                                                                             
Set processes = wmi.ExecQuery("SELECT * FROM Win32_Process WHERE CommandLine LIKE '%spz_logger.exe%'")                                                                                        
For Each process In processes                                                                                                                                                                
    process.Terminate                                     
    count = count + 1
Next

If count > 0 Then
    MsgBox "SPZ Logger stopped (" & count & " process killed).", vbInformation, "SPZ Logger"
Else
    MsgBox "No running SPZ Logger process found.", vbExclamation, "SPZ Logger"
End If
