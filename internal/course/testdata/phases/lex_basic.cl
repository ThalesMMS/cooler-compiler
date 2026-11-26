(* nested (* comments *) are skipped *)
class Main inherits IO {
  -- line comment
  s : String <- "tab\there\
newline";
  n : Int <- 007;
  b : Bool <- tRUE;
  main() : Object { if n <= 3 then out_string(s) else not b fi };
};
